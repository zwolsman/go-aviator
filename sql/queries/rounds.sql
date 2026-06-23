-- name: CreateRound :one
INSERT INTO rounds (commit_hash, crash_multiplier, salt, state)
VALUES ($1, $2, $3, 'betting')
RETURNING *;

-- name: UpdateRoundState :one
UPDATE rounds SET state = $2 WHERE id = $1 RETURNING *;

-- name: RevealOutcome :one
UPDATE rounds
SET state = 'crashed',
    crashed_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SettleRound :one
UPDATE rounds
SET state = 'settled',
    settled_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetRoundForVerification :one
SELECT id, commit_hash, crash_multiplier, salt, state, started_at, crashed_at, settled_at
FROM rounds
WHERE id = $1;

-- name: ListRecentRounds :many
SELECT id, commit_hash, crash_multiplier, salt, state, started_at, crashed_at, settled_at
FROM rounds
WHERE state IN ('crashed', 'settled')
ORDER BY id DESC
LIMIT $1;

-- name: ListRecentRoundsWithStats :many
SELECT
    r.id,
    r.commit_hash,
    r.crash_multiplier,
    r.salt,
    r.state,
    r.started_at,
    r.crashed_at,
    r.settled_at,
    COALESCE(SUM(b.amount), 0)::bigint  AS total_pot,
    COALESCE(SUM(b.payout), 0)::bigint  AS total_payout
FROM rounds r
LEFT JOIN bets b ON b.round_id = r.id
WHERE r.state IN ('crashed', 'settled')
GROUP BY r.id
ORDER BY r.id DESC
LIMIT $1;
