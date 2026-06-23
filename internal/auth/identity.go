package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sync"

	cssh "github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
)

const DailyCreditGrant = 1000 // credits per UTC day
const StartingBalance = 2000  // credits given to a new player

// Manager handles player identity, daily credits, and concurrent session enforcement.
type Manager struct {
	db      *dbpkg.Queries
	mu      sync.Mutex
	active  map[string]bool // fingerprint -> has active session
}

// New creates a new identity Manager.
func New(db *dbpkg.Queries) *Manager {
	return &Manager{
		db:     db,
		active: make(map[string]bool),
	}
}

// Fingerprint computes a stable hex fingerprint for a public key.
func Fingerprint(key cssh.PublicKey) string {
	raw := key.Marshal()
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// FingerprintGo computes a fingerprint from a golang.org/x/crypto ssh public key.
func FingerprintGo(key gossh.PublicKey) string {
	raw := key.Marshal()
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// DisplayName extracts the comment from an SSH public key (used as display name).
func DisplayName(key cssh.PublicKey) string {
	// charmbracelet/ssh wraps golang.org/x/crypto/ssh
	type commentKey interface {
		Comment() string
	}
	// authorized key format includes comment; we use the type string as fallback
	return key.Type()
}

// Login retrieves or creates the player for this key fingerprint, grants daily
// credit if eligible, and registers an active session.
// Returns (player, alreadyActive, isNew, error).
func (m *Manager) Login(ctx context.Context, fp string, displayName string) (dbpkg.Player, bool, bool, error) {
	m.mu.Lock()
	if m.active[fp] {
		m.mu.Unlock()
		return dbpkg.Player{}, true, false, nil
	}
	m.active[fp] = true
	m.mu.Unlock()

	player, isNew, err := m.getOrCreate(ctx, fp, displayName)
	if err != nil {
		m.mu.Lock()
		delete(m.active, fp)
		m.mu.Unlock()
		return dbpkg.Player{}, false, false, err
	}

	// attempt daily credit (no-op if already granted today)
	credited, err := m.db.GrantDailyCredit(ctx, dbpkg.GrantDailyCreditParams{
		ID:      player.ID,
		Balance: DailyCreditGrant,
	})
	if err == nil {
		player = credited
	} else if err != sql.ErrNoRows {
		// sql.ErrNoRows means condition not met (already credited today); ignore
	}

	return player, false, isNew, nil
}

// Logout deregisters the active session for this fingerprint.
func (m *Manager) Logout(fp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, fp)
}

func (m *Manager) getOrCreate(ctx context.Context, fp string, displayName string) (dbpkg.Player, bool, error) {
	player, err := m.db.GetPlayerByFingerprint(ctx, fp)
	if err == nil {
		return player, false, nil
	}
	if err != sql.ErrNoRows {
		return dbpkg.Player{}, false, err
	}
	p, err := m.db.CreatePlayer(ctx, dbpkg.CreatePlayerParams{
		PubkeyFingerprint: fp,
		DisplayName:       displayName,
		Balance:           StartingBalance,
	})
	return p, true, err
}
