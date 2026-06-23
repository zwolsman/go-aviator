-- name: GetPlayerByFingerprint :one
SELECT * FROM players WHERE pubkey_fingerprint = $1;

-- name: CreatePlayer :one
INSERT INTO players (pubkey_fingerprint, display_name, balance)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GrantDailyCredit :one
UPDATE players
SET balance = balance + $2,
    last_credit_date = CURRENT_DATE AT TIME ZONE 'UTC'
WHERE id = $1
  AND (last_credit_date IS NULL OR last_credit_date < CURRENT_DATE AT TIME ZONE 'UTC')
RETURNING *;

-- name: AdjustBalance :one
UPDATE players
SET balance = balance + $2
WHERE id = $1
RETURNING *;

-- name: UpdateDisplayName :one
UPDATE players SET display_name = $1 WHERE id = $2 RETURNING *;
