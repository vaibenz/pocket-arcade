CREATE TABLE IF NOT EXISTS runs (
    session_id  TEXT PRIMARY KEY,
    game        TEXT NOT NULL,
    player_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    score       INTEGER NOT NULL CHECK (score >= 0),
    duration_ms INTEGER NOT NULL CHECK (duration_ms >= 0),
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS runs_player_idx ON runs (game, player_id, created_at DESC);

CREATE TABLE IF NOT EXISTS best_scores (
    game       TEXT NOT NULL,
    player_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    score      INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (game, player_id)
);

CREATE INDEX IF NOT EXISTS best_scores_rank_idx ON best_scores (game, score DESC, player_id DESC);
