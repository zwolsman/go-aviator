package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"sync"
	"time"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
)

const (
	bettingDuration  = 8 * time.Second
	settlePause      = 5 * time.Second
	tickInterval     = 100 * time.Millisecond
	subChanCap       = 4
	dailyCreditGrant = 1000 // credits given once per UTC day
)

// Command types sent from sessions into the engine inbox.
type cmdKind int

const (
	cmdPlaceBet cmdKind = iota
	cmdCashOut
	cmdSubscribe
	cmdUnsubscribe
)

type command struct {
	kind        cmdKind
	playerID    int64
	displayName string
	amount      int64
	autoCashout int // basis points; 0 = manual only
	sub         *subscriber
}

type subscriber struct {
	ch       chan Snapshot
	playerID int64
}

// Engine is the authoritative game engine. All state mutations happen here.
type Engine struct {
	db      *dbpkg.Queries
	sqlDB   *sql.DB
	inbox   chan command
	mu      sync.Mutex
	subs    map[int64]*subscriber // keyed by playerID

	// round state (owned by runLoop goroutine, no lock needed)
	state       State
	roundID     int64
	commitHash  string
	crashBP     int
	salt        string
	bettingEnd  time.Time
	currentBP   int // current multiplier in basis points
	ticks       int // ticks since FLYING started
	participants map[int64]*Participant
}

// New creates and starts the engine. Call Stop to shut it down.
func New(db *dbpkg.Queries, sqlDB *sql.DB) *Engine {
	e := &Engine{
		db:           db,
		sqlDB:        sqlDB,
		inbox:        make(chan command, 64),
		subs:         make(map[int64]*subscriber),
		state:        StateIdle,
		participants: make(map[int64]*Participant),
	}
	go e.runLoop()
	return e
}

// Subscribe registers a session to receive snapshots. Returns a channel and
// an unsubscribe function. The channel is non-blocking; stale frames are dropped.
func (e *Engine) Subscribe(playerID int64, displayName string) (<-chan Snapshot, func()) {
	sub := &subscriber{
		ch:       make(chan Snapshot, subChanCap),
		playerID: playerID,
	}
	e.inbox <- command{kind: cmdSubscribe, playerID: playerID, displayName: displayName, sub: sub}
	return sub.ch, func() {
		e.inbox <- command{kind: cmdUnsubscribe, playerID: playerID, sub: sub}
	}
}

// PlaceBet asks the engine to record a bet for a player.
func (e *Engine) PlaceBet(playerID int64, displayName string, amount int64, autoCashoutBP int) {
	e.inbox <- command{
		kind:        cmdPlaceBet,
		playerID:    playerID,
		displayName: displayName,
		amount:      amount,
		autoCashout: autoCashoutBP,
	}
}

// CashOut asks the engine to cash out a player at the current multiplier.
func (e *Engine) CashOut(playerID int64) {
	e.inbox <- command{kind: cmdCashOut, playerID: playerID}
}

func (e *Engine) runLoop() {
	for {
		switch e.state {
		case StateIdle:
			e.waitForSubscribers()
		case StateBetting:
			e.runBetting()
		case StateFlying:
			e.runFlying()
		case StateCrashed:
			e.runSettle()
		}
	}
}

// waitForSubscribers blocks until at least one subscriber connects.
func (e *Engine) waitForSubscribers() {
	for {
		cmd := <-e.inbox
		if cmd.kind == cmdSubscribe {
			e.addSubscriber(cmd)
			e.startRound()
			e.broadcast() // send initial betting snapshot so client sees the form immediately
			return
		}
	}
}

func (e *Engine) startRound() {
	e.participants = make(map[int64]*Participant)
	e.ticks = 0
	e.currentBP = 100

	// generate and persist the round
	e.crashBP = sampleCrash()
	e.salt = randomSalt()

	row, err := e.db.CreateRound(context.Background(), dbpkg.CreateRoundParams{
		CommitHash:      "pending", // placeholder; updated below after we have the ID
		CrashMultiplier: int32(e.crashBP),
		Salt:            e.salt,
	})
	if err != nil {
		slog.Error("create round failed", "err", err)
		e.state = StateIdle
		return
	}
	e.roundID = row.ID
	_, e.commitHash = Commit(e.roundID, e.crashBP, e.salt)

	// update commit_hash now that we have the real ID
	if _, err := e.sqlDB.ExecContext(context.Background(),
		`UPDATE rounds SET commit_hash=$1 WHERE id=$2`, e.commitHash, e.roundID); err != nil {
		slog.Error("update commit_hash failed", "err", err)
	}

	e.bettingEnd = time.Now().Add(bettingDuration)
	e.state = StateBetting
	slog.Info("round started", "roundID", e.roundID, "commitHash", e.commitHash)
}

func (e *Engine) runBetting() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		remaining := time.Until(e.bettingEnd)
		if remaining <= 0 {
			e.commitBetsAndTakeoff()
			return
		}
		select {
		case cmd := <-e.inbox:
			e.handleCmd(cmd)
		case <-ticker.C:
			e.broadcast()
		case <-time.After(remaining):
			e.commitBetsAndTakeoff()
			return
		}
	}
}

func (e *Engine) commitBetsAndTakeoff() {
	if len(e.participants) == 0 {
		// no bets this round — still run the flying phase so spectators can watch
		if _, err := e.sqlDB.ExecContext(context.Background(),
			`UPDATE rounds SET state='flying' WHERE id=$1`, e.roundID); err != nil {
			slog.Error("update round state (no bets) failed", "err", err)
		}
		e.state = StateFlying
		slog.Info("takeoff (no bets)", "roundID", e.roundID)
		return
	}

	ctx := context.Background()
	tx, err := e.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin tx failed", "err", err)
		e.state = StateIdle
		return
	}
	qtx := e.db.WithTx(tx)

	for _, p := range e.participants {
		// debit balance
		if _, err := qtx.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{
			ID:      p.PlayerID,
			Balance: -p.Amount,
		}); err != nil {
			tx.Rollback()
			slog.Error("debit failed", "player", p.PlayerID, "err", err)
			e.state = StateIdle
			return
		}
		// create bet row
		var ac sql.NullInt32
		if p.AutoCashout > 0 {
			ac = sql.NullInt32{Int32: int32(p.AutoCashout), Valid: true}
		}
		bet, err := qtx.CreateBet(ctx, dbpkg.CreateBetParams{
			RoundID:     e.roundID,
			PlayerID:    p.PlayerID,
			Amount:      p.Amount,
			AutoCashout: ac,
		})
		if err != nil {
			tx.Rollback()
			slog.Error("create bet failed", "player", p.PlayerID, "err", err)
			e.state = StateIdle
			return
		}
		p.BetID = bet.ID
	}

	// update round state to flying
	if _, err := e.sqlDB.ExecContext(ctx, `UPDATE rounds SET state='flying' WHERE id=$1`, e.roundID); err != nil {
		tx.Rollback()
		slog.Error("update round state failed", "err", err)
		e.state = StateIdle
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("commit tx failed", "err", err)
		e.state = StateIdle
		return
	}

	e.state = StateFlying
	slog.Info("takeoff", "roundID", e.roundID, "players", len(e.participants))
}

func (e *Engine) runFlying() {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case cmd := <-e.inbox:
			e.handleCmd(cmd)
		case <-ticker.C:
			e.ticks++
			// exponential growth: mult = e^(k*ticks)
			rawMult := math.Exp(growthK * float64(e.ticks))
			e.currentBP = int(rawMult * 100)

			// cashout check BEFORE crash (>= tie-break: cashout wins)
			// Use the configured auto-cashout value, not the current tick, so that
			// overshooting a threshold doesn't inflate the payout beyond what was set.
			for _, p := range e.participants {
				if p.CashedOut {
					continue
				}
				if p.AutoCashout > 0 && e.currentBP >= p.AutoCashout {
					e.settleCashout(p, p.AutoCashout)
				}
			}

			// crash check
			if e.currentBP >= e.crashBP {
				e.state = StateCrashed
				e.broadcast()
				e.revealOutcome()
				return
			}

			e.broadcast()
		}
	}
}

func (e *Engine) settleCashout(p *Participant, multBP int) {
	p.CashedOut = true
	p.CashoutMult = multBP
	p.Payout = p.Amount * int64(multBP) / 100
	slog.Info("cashout", "player", p.PlayerID, "mult", FormatMult(multBP), "payout", p.Payout)
}

func (e *Engine) revealOutcome() {
	ctx := context.Background()
	if _, err := e.db.RevealOutcome(ctx, e.roundID); err != nil {
		slog.Error("reveal outcome failed", "err", err)
	}
}

func (e *Engine) runSettle() {
	ctx := context.Background()
	tx, err := e.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin settle tx failed", "err", err)
		e.state = StateIdle
		return
	}
	qtx := e.db.WithTx(tx)

	for _, p := range e.participants {
		if p.BetID == 0 {
			// bet was never committed (e.g. nobody bet this round)
			continue
		}
		var cashoutMult sql.NullInt32
		var payout sql.NullInt64
		if p.CashedOut {
			cashoutMult = sql.NullInt32{Int32: int32(p.CashoutMult), Valid: true}
			payout = sql.NullInt64{Int64: p.Payout, Valid: true}
			// credit payout to player
			if _, err := qtx.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{
				ID:      p.PlayerID,
				Balance: p.Payout,
			}); err != nil {
				tx.Rollback()
				slog.Error("credit failed", "player", p.PlayerID, "err", err)
				e.state = StateIdle
				return
			}
		}
		if _, err := qtx.SettleBet(ctx, dbpkg.SettleBetParams{
			ID:                    p.BetID,
			CashedOutAtMultiplier: cashoutMult,
			Payout:                payout,
		}); err != nil {
			tx.Rollback()
			slog.Error("settle bet failed", "player", p.PlayerID, "err", err)
			e.state = StateIdle
			return
		}
	}

	if _, err := e.db.SettleRound(ctx, e.roundID); err != nil {
		tx.Rollback()
		slog.Error("settle round failed", "err", err)
		e.state = StateIdle
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("settle commit failed", "err", err)
		e.state = StateIdle
		return
	}

	slog.Info("round settled", "roundID", e.roundID)
	e.broadcast() // one final broadcast with settled state

	// pause to let players see results
	time.Sleep(settlePause)

	e.mu.Lock()
	hasSubs := len(e.subs) > 0
	e.mu.Unlock()

	if hasSubs {
		e.startRound()
	} else {
		e.state = StateIdle
	}
}

func (e *Engine) handleCmd(cmd command) {
	switch cmd.kind {
	case cmdSubscribe:
		e.addSubscriber(cmd)
		// send current snapshot immediately
		e.sendTo(e.subs[cmd.playerID], e.snapshot())

	case cmdUnsubscribe:
		e.mu.Lock()
		delete(e.subs, cmd.playerID)
		e.mu.Unlock()

	case cmdPlaceBet:
		if e.state != StateBetting {
			return
		}
		if _, exists := e.participants[cmd.playerID]; exists {
			return // already bet
		}
		e.participants[cmd.playerID] = &Participant{
			PlayerID:    cmd.playerID,
			DisplayName: cmd.displayName,
			Amount:      cmd.amount,
			AutoCashout: cmd.autoCashout,
		}
		e.broadcast()

	case cmdCashOut:
		if e.state != StateFlying {
			return
		}
		p, ok := e.participants[cmd.playerID]
		if !ok || p.CashedOut {
			return
		}
		e.settleCashout(p, e.currentBP)
		e.broadcast()
	}
}

func (e *Engine) addSubscriber(cmd command) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.subs[cmd.playerID] = cmd.sub
}

func (e *Engine) broadcast() {
	snap := e.snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, sub := range e.subs {
		e.sendTo(sub, snap)
	}
}

func (e *Engine) sendTo(sub *subscriber, snap Snapshot) {
	if sub == nil {
		return
	}
	select {
	case sub.ch <- snap:
	default:
		// drain one stale frame and try again
		select {
		case <-sub.ch:
		default:
		}
		select {
		case sub.ch <- snap:
		default:
		}
	}
}

func (e *Engine) snapshot() Snapshot {
	views := make([]ParticipantView, 0, len(e.participants))
	for _, p := range e.participants {
		views = append(views, ParticipantView{
			PlayerID:    p.PlayerID,
			DisplayName: p.DisplayName,
			Amount:      p.Amount,
			CashedOut:   p.CashedOut,
			CashoutMult: p.CashoutMult,
			Payout:      p.Payout,
		})
	}

	snap := Snapshot{
		State:         e.state,
		RoundID:       e.roundID,
		CommitHash:    e.commitHash,
		CurrentMultBP: e.currentBP,
		Participants:  views,
		BettingEndsAt: e.bettingEnd,
	}
	// only reveal crash details after crash
	if e.state == StateCrashed || e.state == StateSettling {
		snap.CrashMultBP = e.crashBP
		snap.Salt = e.salt
	}
	return snap
}
