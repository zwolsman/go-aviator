//go:build integration

package engine

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/internal/store"
)

// Run with: go test -tags=integration ./internal/engine/... -run TestBetSettleIntegration
// Requires DATABASE_URL environment variable or defaults to localhost:5433.

func TestBetSettleIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://crashgame:crashgame@localhost:5433/crashgame?sslmode=disable"
	}

	sqlDB, queries, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()

	// Create two test players
	p1, err := queries.CreatePlayer(ctx, dbpkg.CreatePlayerParams{
		PubkeyFingerprint: "test-fp-p1-" + time.Now().Format("150405"),
		DisplayName:       "test-alice",
		Balance:           5000,
	})
	if err != nil {
		t.Fatalf("create player 1: %v", err)
	}
	p2, err := queries.CreatePlayer(ctx, dbpkg.CreatePlayerParams{
		PubkeyFingerprint: "test-fp-p2-" + time.Now().Format("150405"),
		DisplayName:       "test-bob",
		Balance:           5000,
	})
	if err != nil {
		t.Fatalf("create player 2: %v", err)
	}

	// Create a round
	crashBP := 250 // 2.50x crash point
	salt := "test-salt-abc123"
	_, commitHash := Commit(int64(0), crashBP, salt) // ID will be assigned

	round, err := queries.CreateRound(ctx, dbpkg.CreateRoundParams{
		CommitHash:      "pending",
		CrashMultiplier: int32(crashBP),
		Salt:            salt,
	})
	if err != nil {
		t.Fatalf("create round: %v", err)
	}

	// Update commit hash with real ID
	_, err = sqlDB.ExecContext(ctx, `UPDATE rounds SET commit_hash=$1 WHERE id=$2`, commitHash, round.ID)
	if err != nil {
		t.Fatalf("update commit hash: %v", err)
	}
	// Re-compute with the real round ID
	_, commitHash = Commit(round.ID, crashBP, salt)
	_, err = sqlDB.ExecContext(ctx, `UPDATE rounds SET commit_hash=$1 WHERE id=$2`, commitHash, round.ID)
	if err != nil {
		t.Fatalf("update commit hash (round id): %v", err)
	}

	t.Logf("round %d: crash=%s commit=%s", round.ID, FormatMult(crashBP), commitHash[:16]+"...")

	// === BETTING PHASE: debit both players and insert bet rows ===
	betAmount := int64(500)

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	qtx := queries.WithTx(tx)

	// Debit p1
	p1After, err := qtx.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{ID: p1.ID, Balance: -betAmount})
	if err != nil {
		tx.Rollback()
		t.Fatalf("debit p1: %v", err)
	}
	// Debit p2
	p2After, err := qtx.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{ID: p2.ID, Balance: -betAmount})
	if err != nil {
		tx.Rollback()
		t.Fatalf("debit p2: %v", err)
	}

	// Create bets: p1 has auto-cashout at 2.00x, p2 has no auto-cashout
	autoCashout200 := sql.NullInt32{Int32: 200, Valid: true}
	bet1, err := qtx.CreateBet(ctx, dbpkg.CreateBetParams{
		RoundID: round.ID, PlayerID: p1.ID, Amount: betAmount, AutoCashout: autoCashout200,
	})
	if err != nil {
		tx.Rollback()
		t.Fatalf("create bet1: %v", err)
	}
	bet2, err := qtx.CreateBet(ctx, dbpkg.CreateBetParams{
		RoundID: round.ID, PlayerID: p2.ID, Amount: betAmount, AutoCashout: sql.NullInt32{},
	})
	if err != nil {
		tx.Rollback()
		t.Fatalf("create bet2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit betting tx: %v", err)
	}

	if p1After.Balance != 4500 {
		t.Errorf("p1 balance after debit = %d, want 4500", p1After.Balance)
	}
	if p2After.Balance != 4500 {
		t.Errorf("p2 balance after debit = %d, want 4500", p2After.Balance)
	}

	// === FLYING PHASE: p1 auto-cashes at 2.00x; p2 rides to crash at 2.50x and loses ===

	// p1 cashes out at 2.00x: payout = 500 * 200 / 100 = 1000
	p1Payout := int64(500 * 200 / 100) // 1000
	// p2 loses: payout = 0

	// === SETTLE PHASE ===
	tx2, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin settle tx: %v", err)
	}
	qtx2 := queries.WithTx(tx2)

	// Credit p1's payout
	p1Final, err := qtx2.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{ID: p1.ID, Balance: p1Payout})
	if err != nil {
		tx2.Rollback()
		t.Fatalf("credit p1: %v", err)
	}

	// Settle bet1 (p1 cashed out at 2.00x)
	if _, err := qtx2.SettleBet(ctx, dbpkg.SettleBetParams{
		ID:                    bet1.ID,
		CashedOutAtMultiplier: sql.NullInt32{Int32: 200, Valid: true},
		Payout:                sql.NullInt64{Int64: p1Payout, Valid: true},
	}); err != nil {
		tx2.Rollback()
		t.Fatalf("settle bet1: %v", err)
	}

	// Settle bet2 (p2 lost)
	if _, err := qtx2.SettleBet(ctx, dbpkg.SettleBetParams{
		ID:                    bet2.ID,
		CashedOutAtMultiplier: sql.NullInt32{},
		Payout:                sql.NullInt64{},
	}); err != nil {
		tx2.Rollback()
		t.Fatalf("settle bet2: %v", err)
	}

	// Settle the round
	if _, err := qtx2.SettleRound(ctx, round.ID); err != nil {
		tx2.Rollback()
		t.Fatalf("settle round: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit settle tx: %v", err)
	}

	// === VERIFY FINAL STATE ===

	// p1: started 5000, bet 500, won 1000 → final 5500
	if p1Final.Balance != 5500 {
		t.Errorf("p1 final balance = %d, want 5500 (5000 - 500 + 1000)", p1Final.Balance)
	}

	// p2: started 5000, bet 500, lost → final 4500
	p2Final, err := queries.AdjustBalance(ctx, dbpkg.AdjustBalanceParams{ID: p2.ID, Balance: 0})
	if err != nil {
		t.Fatalf("get p2 balance: %v", err)
	}
	if p2Final.Balance != 4500 {
		t.Errorf("p2 final balance = %d, want 4500 (5000 - 500)", p2Final.Balance)
	}

	// Verify round state in DB
	settledRound, err := queries.GetRoundForVerification(ctx, round.ID)
	if err != nil {
		t.Fatalf("get round: %v", err)
	}
	if settledRound.State != "settled" {
		t.Errorf("round state = %s, want settled", settledRound.State)
	}

	// Fairness audit: independently verify the commit hash
	preimage, recomputed := Commit(round.ID, crashBP, salt)
	if recomputed != commitHash {
		t.Errorf("commit hash mismatch!\n  preimage:   %s\n  recomputed: %s\n  stored:     %s",
			preimage, recomputed, commitHash)
	}
	if !Verify(round.ID, crashBP, salt, commitHash) {
		t.Error("Verify() returned false for a valid round")
	}
	t.Logf("preimage: %s", preimage)
	t.Logf("commit verified: ✓")

	t.Logf("p1 final balance: %d (bet 500, won 1000 @2.00x)", p1Final.Balance)
	t.Logf("p2 final balance: %d (bet 500, lost @2.50x crash)", p2Final.Balance)
}
