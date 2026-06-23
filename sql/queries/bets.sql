-- name: CreateBet :one
INSERT INTO bets (round_id, player_id, amount, auto_cashout)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SettleBet :one
UPDATE bets
SET cashed_out_at_multiplier = $2,
    payout = $3
WHERE id = $1
RETURNING *;

-- name: GetBetsForRound :many
SELECT b.*, p.pubkey_fingerprint, p.display_name
FROM bets b
JOIN players p ON p.id = b.player_id
WHERE b.round_id = $1;
