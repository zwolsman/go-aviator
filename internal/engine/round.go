package engine

import "time"

// State represents the engine's FSM state.
type State int

const (
	StateIdle    State = iota
	StateBetting       // accepting bets; commit_hash published
	StateFlying        // plane is in the air; multiplier climbing
	StateCrashed       // plane crashed; outcome revealed
	StateSettling      // writing payouts to DB; brief display pause
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateBetting:
		return "betting"
	case StateFlying:
		return "flying"
	case StateCrashed:
		return "crashed"
	case StateSettling:
		return "settling"
	default:
		return "unknown"
	}
}

// Participant is one player in the current round.
type Participant struct {
	PlayerID    int64
	DisplayName string
	Amount      int64  // bet amount (credits)
	AutoCashout int    // basis points; 0 = no auto cashout
	BetID       int64  // DB row ID (after commit)
	CashedOut   bool
	CashoutMult int    // basis points at which they cashed out; 0 = still in / lost
	Payout      int64
}

// Snapshot is an immutable view of the engine state broadcast to each session.
type Snapshot struct {
	State          State
	RoundID        int64
	CommitHash     string  // only populated during Betting/Flying
	CurrentMultBP  int     // current multiplier in basis points (100 = 1.00x)
	CrashMultBP    int     // revealed after crash; 0 while hidden
	Salt           string  // revealed after crash
	BettingEndsAt  time.Time
	Participants   []ParticipantView
}

// ParticipantView is a read-only view of a participant for the TUI.
type ParticipantView struct {
	PlayerID    int64
	DisplayName string
	Amount      int64
	CashedOut   bool
	CashoutMult int   // 0 = still in or lost
	Payout      int64
}

// CrashAt returns the number of milliseconds until the plane reaches a given
// multiplier under the exponential growth curve e^(k*t).
// k is chosen so the multiplier doubles roughly every 10s.
const growthK = 0.07 // per 100ms tick; gives ~1.07x per tick, doubling ~10.5s
