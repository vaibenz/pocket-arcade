package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory implements both Durable and Board in process. It backs unit tests
// and `-store=memory` for running the server without any dependencies.
type Memory struct {
	mu       sync.Mutex
	runs     map[string]Run
	best     map[string]map[string]Entry // game -> player -> best
	claimed  map[string]time.Time
	FailNext error // when set, the next Board call returns it (for tests)
}

func NewMemory() *Memory {
	return &Memory{runs: map[string]Run{}, best: map[string]map[string]Entry{}, claimed: map[string]time.Time{}}
}

func (m *Memory) fail() error {
	err := m.FailNext
	m.FailNext = nil
	return err
}

func (m *Memory) SaveRun(_ context.Context, r Run) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.runs[r.SessionID]; dup {
		return 0, 0, ErrDuplicateSession
	}
	prev := -1
	if e, ok := m.best[r.Game][r.PlayerID]; ok {
		prev = e.Score
	}
	m.runs[r.SessionID] = r
	m.offerLocked(r.Game, r.PlayerID, r.Name, r.Score)
	return prev, m.best[r.Game][r.PlayerID].Score, nil
}

func (m *Memory) offerLocked(game, pid, name string, score int) {
	g := m.best[game]
	if g == nil {
		g = map[string]Entry{}
		m.best[game] = g
	}
	if e, ok := g[pid]; !ok || score > e.Score {
		g[pid] = Entry{PlayerID: pid, Name: name, Score: score}
	}
}

func (m *Memory) sorted(game string) []Entry {
	var out []Entry
	for _, e := range m.best[game] {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].PlayerID > out[j].PlayerID // same tie order as ZREVRANGE
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func (m *Memory) TopBest(_ context.Context, game string, n int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.sorted(game)
	return all[:min(n, len(all))], nil
}

func (m *Memory) AllBest(ctx context.Context, game string) ([]Entry, error) {
	return m.TopBest(ctx, game, 1<<30)
}

func (m *Memory) ClaimSession(_ context.Context, sid string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail(); err != nil {
		return false, err
	}
	if _, ok := m.claimed[sid]; ok {
		return false, nil
	}
	m.claimed[sid] = time.Now().Add(ttl)
	return true, nil
}

func (m *Memory) Offer(_ context.Context, game, pid, name string, score int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail(); err != nil {
		return err
	}
	m.offerLocked(game, pid, name, score)
	return nil
}

func (m *Memory) Top(ctx context.Context, game string, n int) ([]Entry, error) {
	m.mu.Lock()
	err := m.fail()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return m.TopBest(ctx, game, n)
}

func (m *Memory) Rank(_ context.Context, game, pid string) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail(); err != nil {
		return 0, false, err
	}
	for _, e := range m.sorted(game) {
		if e.PlayerID == pid {
			return e.Rank, true, nil
		}
	}
	return 0, false, nil
}

func (m *Memory) Exists(_ context.Context, game string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.best[game]) > 0, nil
}

func (m *Memory) Load(_ context.Context, game string, entries []Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		m.offerLocked(game, e.PlayerID, e.Name, e.Score)
	}
	return nil
}

func (m *Memory) Ping(context.Context) error { return nil }
