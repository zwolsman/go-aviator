CREATE TABLE players (
    id                BIGSERIAL PRIMARY KEY,
    pubkey_fingerprint TEXT NOT NULL UNIQUE,
    display_name      TEXT NOT NULL DEFAULT '',
    balance           BIGINT NOT NULL DEFAULT 0,
    last_credit_date  DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE rounds (
    id                BIGSERIAL PRIMARY KEY,
    commit_hash       TEXT NOT NULL,
    crash_multiplier  INTEGER NOT NULL,
    salt              TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT 'betting',
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    crashed_at        TIMESTAMPTZ,
    settled_at        TIMESTAMPTZ
);

CREATE TABLE bets (
    id                      BIGSERIAL PRIMARY KEY,
    round_id                BIGINT NOT NULL REFERENCES rounds(id),
    player_id               BIGINT NOT NULL REFERENCES players(id),
    amount                  BIGINT NOT NULL,
    auto_cashout            INTEGER,
    cashed_out_at_multiplier INTEGER,
    payout                  BIGINT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (round_id, player_id)
);

CREATE INDEX idx_bets_round_id ON bets(round_id);
CREATE INDEX idx_bets_player_id ON bets(player_id);
