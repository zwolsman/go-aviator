# ✈ Aviator

A multiplayer crash-style betting game playable entirely over SSH. No browser, no app — just a terminal.

```
ssh -p 2222 play.example.com
```

Players connect with their SSH key (no password required). Each key is a unique identity. A daily credit allowance is granted automatically.

---

## Game loop

Every round follows the same cycle:

```
BETTING (5s) → FLYING → CRASHED → SETTLE → repeat
```

1. **Betting** — a countdown opens the window. Players enter a bet amount and an optional auto-cashout multiplier. The commitment hash for the round is published at this point, proving the outcome is already fixed.
2. **Flying** — the plane takes off. A multiplier climbs from 1.00x upward on an exponential curve. Players who set an auto-cashout are paid out automatically when the multiplier crosses their target. Everyone else can press `SPACE` to cash out manually.
3. **Crashed** — the plane flies away at a pre-determined multiplier. Anyone still in loses their stake.
4. **Settle** — payouts are written to the database. Winners' balances are credited. The round result is revealed for verification.

The engine pauses when nobody is connected and resumes the moment a player joins — no rounds are burned on an empty server.

---

## Controls

| Key | Action |
|-----|--------|
| `SPACE` | Cash out (during flight) |
| `h` | Open round history |
| `i` | Inspect current round (after crash) |
| `s` | Settings (display name) |
| `q` | Quit |
| `↑ / ↓` | Navigate history |
| `enter` | Open round detail in history |
| `esc / q` | Back / exit screen |

---

## Provably fair

Before betting opens, the engine commits to the crash outcome by publishing:

```
commit_hash = SHA256("GAME-<id>-MULT-<multiplier>-<salt>")
```

The multiplier and salt stay hidden until after the crash. Once revealed, any player can recompute the hash and confirm it matches what was shown at betting-open — proving the result was fixed before any bets were placed.

The in-game inspect screen (`i`) does this automatically and shows ✓ VERIFIED or ✗ MISMATCH.

To verify manually:

```sh
echo -n 'GAME-42-MULT-2.34-<salt>' | sha256sum
```

---

## Architecture

```
┌─────────────────── Game Engine (single goroutine) ────────────────────┐
│  State machine: IDLE → BETTING → FLYING → CRASHED → SETTLE → repeat   │
│  Owns crash point, multiplier clock, participant map.                  │
│  Persists rounds and bets to Postgres.                                 │
└────────────────────────┬──────────────────────────────────────────────┘
                         │ broadcasts immutable snapshots ~100ms
          ┌──────────────┼──────────────┐
       Session A      Session B      Session C          (SSH + Bubble Tea)
       each session subscribes on connect, unsubscribes on disconnect
       commands (PlaceBet, CashOut) are sent back to the engine inbox
```

### Key properties

- **Single authoritative engine** — no shared mutable state between sessions. The engine is the sole writer of round state; sessions are read-only subscribers plus command senders.
- **Drop-stale backpressure** — snapshot sends to subscriber channels are non-blocking. If a client's buffer is full the engine drops the stale frame for that client only and never stalls the engine or other sessions.
- **Server-authoritative cashout** — once a bet is committed at takeoff, the engine owns it. Auto-cashout fires at the configured multiplier regardless of whether the player is still connected. A disconnected player with no auto-cashout rides to the crash.
- **Durability boundary** — bets are held in memory during betting, then committed to Postgres in a single transaction at takeoff (BETTING→FLYING). Payouts are written in a second transaction at SETTLE. A server crash during betting loses nothing; a crash after takeoff preserves all bets.

### Stack

| Concern | Choice |
|---------|--------|
| Language | Go 1.26 |
| SSH server | [Wish](https://github.com/charmbracelet/wish) |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss) |
| Database | Postgres via `pgx/v5` |
| Migrations | `golang-migrate` (auto-applied on boot) |
| Query layer | `sqlc` (type-safe generated code) |

---

## Running locally

**Prerequisites:** Docker, Go 1.26+

```sh
# Start Postgres
docker compose up -d

# Run the server (host key is generated automatically on first run)
go run ./cmd/server
```

Connect from another terminal:

```sh
ssh -p 2222 -i ~/.ssh/id_ed25519 localhost
```

A second connection with the same key is rejected — one active session per identity.

---

## Deployment

### Docker

```sh
docker build -t ghcr.io/zwolsman/go-aviator:latest .
docker run -p 2222:2222 \
  -e DATABASE_URL='postgres://...' \
  -v /path/to/keys:/app/keys:ro \
  ghcr.io/zwolsman/go-aviator:latest
```

### Kubernetes (kustomize)

The SSH host key must never be committed to git. Generate it once and keep it as a Kubernetes Secret:

```sh
# Generate (or copy your existing key)
ssh-keygen -t ed25519 -f keys/host_ed25519 -N ""

# Create the database URL secret
kubectl create secret generic go-aviator-db \
  --from-literal=url='postgres://user:pass@host:5432/aviator?sslmode=require'

# Build manifests (kustomize reads the key file and generates the Secret)
kubectl apply -k infra/base/
```

The GitHub Actions workflow builds and pushes `ghcr.io/zwolsman/go-aviator` on every push to `main`.
