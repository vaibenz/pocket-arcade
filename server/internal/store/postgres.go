package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/lib/pq"
)

//go:embed schema.sql
var schema string

type Postgres struct{ db *sql.DB }

func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Postgres{db: db}, nil
}

func (p *Postgres) Close() error { return p.db.Close() }

func (p *Postgres) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func (p *Postgres) SaveRun(ctx context.Context, r Run) (int, int, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO runs (session_id, game, player_id, name, score, duration_ms, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.SessionID, r.Game, r.PlayerID, r.Name, r.Score, r.DurationMs, r.CreatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return 0, 0, ErrDuplicateSession
	}
	if err != nil {
		return 0, 0, err
	}

	// lock the player's row so concurrent runs see a consistent previous best
	prev := -1
	err = tx.QueryRowContext(ctx,
		`SELECT score FROM best_scores WHERE game = $1 AND player_id = $2 FOR UPDATE`,
		r.Game, r.PlayerID).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}

	var best int
	err = tx.QueryRowContext(ctx,
		`INSERT INTO best_scores (game, player_id, name, score, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (game, player_id) DO UPDATE
		   SET score = GREATEST(best_scores.score, EXCLUDED.score),
		       name = EXCLUDED.name,
		       updated_at = CASE WHEN EXCLUDED.score > best_scores.score
		                         THEN EXCLUDED.updated_at ELSE best_scores.updated_at END
		 RETURNING score`,
		r.Game, r.PlayerID, r.Name, r.Score, r.CreatedAt).Scan(&best)
	if err != nil {
		return 0, 0, err
	}
	return prev, best, tx.Commit()
}

func (p *Postgres) TopBest(ctx context.Context, game string, n int) ([]Entry, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT player_id, name, score FROM best_scores
		 WHERE game = $1 ORDER BY score DESC, player_id DESC LIMIT $2`, game, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.PlayerID, &e.Name, &e.Score); err != nil {
			return nil, err
		}
		e.Rank = len(out) + 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) AllBest(ctx context.Context, game string) ([]Entry, error) {
	return p.TopBest(ctx, game, 1_000_000)
}
