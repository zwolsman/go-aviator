-- name: ListLeaderboard :many
SELECT display_name, hidden, balance, games, rank, player_id
FROM (
  SELECT
    p.id AS player_id,
    p.display_name,
    p.hidden,
    p.balance,
    COUNT(b.id)::bigint AS games,
    RANK() OVER (ORDER BY p.balance DESC, p.balance_updated_at ASC)::bigint AS rank
  FROM players p
  JOIN bets b ON b.player_id = p.id
  GROUP BY p.id, p.display_name, p.hidden, p.balance, p.balance_updated_at
) sub
ORDER BY rank
LIMIT $1 OFFSET $2;

-- name: GetPlayerStanding :one
SELECT rank, games, balance, total
FROM (
  SELECT
    p.id AS player_id,
    p.balance,
    COUNT(b.id)::bigint AS games,
    RANK() OVER (ORDER BY p.balance DESC, p.balance_updated_at ASC)::bigint AS rank,
    COUNT(*) OVER ()::bigint AS total
  FROM players p
  JOIN bets b ON b.player_id = p.id
  GROUP BY p.id, p.balance, p.balance_updated_at
) sub
WHERE player_id = $1;
