// Package store keeps scores in two places. PostgreSQL is the record of
// every run and each player's best. Redis holds a sorted set per game that
// answers top N and rank queries in O(log n), and can always be rebuilt from
// PostgreSQL if it is flushed or restarted.
package store

import (
	"context"
	"errors"
	"time"
)

var ErrDuplicateSession = errors.New("session already submitted")

type Run struct {
	SessionID  string
	Game       string
	PlayerID   string
	Name       string
	Score      int
	DurationMs int
	CreatedAt  time.Time
}

type Entry struct {
	Rank     int    `json:"rank"`
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Score    int    `json:"score"`
}

// Durable is the source of truth.
type Durable interface {
	// SaveRun stores the run and raises the player's best if beaten, in one
	// transaction. It returns the player's best before (-1 if none) and after
	// the run, or ErrDuplicateSession if this session was already saved.
	SaveRun(ctx context.Context, r Run) (prev, best int, err error)
	TopBest(ctx context.Context, game string, n int) ([]Entry, error)
	AllBest(ctx context.Context, game string) ([]Entry, error)
	Ping(ctx context.Context) error
}

// Board is the fast leaderboard.
type Board interface {
	// ClaimSession returns false if the session was claimed before.
	ClaimSession(ctx context.Context, sessionID string, ttl time.Duration) (bool, error)
	// Offer records score for the player if it beats their current best.
	Offer(ctx context.Context, game, playerID, name string, score int) error
	Top(ctx context.Context, game string, n int) ([]Entry, error)
	// Rank is 1 based; ok is false if the player has no score.
	Rank(ctx context.Context, game, playerID string) (rank int, ok bool, err error)
	Exists(ctx context.Context, game string) (bool, error)
	Load(ctx context.Context, game string, entries []Entry) error
	Ping(ctx context.Context) error
}
