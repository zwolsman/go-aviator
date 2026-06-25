-- name: CreateBet :one
INSERT INTO bets (round_id, player_id, amount, auto_cashout)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SettleBet :one
UPDATE bets
SET cashed_out_at_multiplier = $2,
    payout = $3
WHERE id = $1
RETURNING id, round_id, player_id, amount, auto_cashout, cashed_out_at_multiplier, payout, created_at;

-- name: GetBetsForRound :many
SELECT b.id, b.round_id, b.player_id, b.amount, b.auto_cashout, b.cashed_out_at_multiplier, b.payout, b.created_at, p.pubkey_fingerprint, p.display_name, p.hidden
FROM bets b
JOIN players p ON p.id = b.player_id
WHERE b.round_id = $1;
