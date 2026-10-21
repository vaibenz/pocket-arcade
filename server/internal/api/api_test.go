package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vaibenz/pocket-arcade/server/internal/session"
	"github.com/vaibenz/pocket-arcade/server/internal/sim"
	"github.com/vaibenz/pocket-arcade/server/internal/store"
)

// clock is a settable time source shared by the signer.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type env struct {
	t     testing.TB
	url   string
	clock *clock
	srv   *Server
	mem   *store.Memory // nil when running against real Redis and PostgreSQL
	reset func()        // drops the leaderboard cache
}

// backends runs fn against the in memory store, and against real PostgreSQL
// and Redis when TEST_DATABASE_URL and TEST_REDIS_URL are set (as in CI).
func backends(t *testing.T, fn func(t *testing.T, e *env)) {
	t.Run("memory", func(t *testing.T) {
		m := store.NewMemory()
		e := newEnv(t, m, m)
		e.mem = m
		e.reset = func() { e.srv.Board = store.NewMemory() }
		fn(t, e)
	})
	dsn, rurl := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_REDIS_URL")
	t.Run("postgres+redis", func(t *testing.T) {
		if dsn == "" || rurl == "" {
			t.Skip("set TEST_DATABASE_URL and TEST_REDIS_URL to run against real services")
		}
		ctx := context.Background()
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, _ = db.Exec(`DROP TABLE IF EXISTS runs, best_scores`)
		pg, err := store.OpenPostgres(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pg.Close()
		opt, _ := redis.ParseURL(rurl)
		raw := redis.NewClient(opt)
		defer raw.Close()
		raw.FlushDB(ctx)
		rd, err := store.OpenRedis(ctx, rurl)
		if err != nil {
			t.Fatal(err)
		}
		defer rd.Close()
		e := newEnv(t, pg, rd)
		e.reset = func() { raw.FlushDB(ctx) }
		fn(t, e)
	})
}

func newEnv(t testing.TB, d store.Durable, b store.Board) *env {
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	srv := &Server{
		Signer:       session.NewSigner([]byte("test-key-0123456789"), 15*time.Minute).WithClock(c.Now),
		Durable:      d,
		Board:        b,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		AllowOrigins: []string{"http://localhost:5173"},
	}
	// route through a closure so tests can swap srv.Board mid test
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	return &env{t: t, url: hs.URL, clock: c, srv: srv}
}

func (e *env) do(method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *env) session(game, pid, name string) sessionResp {
	e.t.Helper()
	var s sessionResp
	if code := e.do("POST", "/v1/sessions", sessionReq{Game: game, PlayerID: pid, Name: name}, &s); code != 201 {
		e.t.Fatalf("create session: %d", code)
	}
	return s
}

type errBody struct {
	Code string `json:"code"`
}

func (e *env) submit(tok string, claimed int, taps any) (int, scoreResp, string) {
	e.t.Helper()
	raw, _ := json.Marshal(taps)
	var resp scoreResp
	b, _ := json.Marshal(scoreReq{Token: tok, ClaimedScore: claimed, Taps: raw})
	req, _ := http.NewRequest("POST", e.url+"/v1/scores", bytes.NewReader(b))
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	var eb errBody
	_ = json.Unmarshal(body, &resp)
	_ = json.Unmarshal(body, &eb)
	return r.StatusCode, resp, eb.Code
}

func (e *env) board(game, query string) leaderboardResp {
	e.t.Helper()
	var lb leaderboardResp
	if code := e.do("GET", "/v1/leaderboards/"+game+query, nil, &lb); code != 200 {
		e.t.Fatalf("leaderboard: %d", code)
	}
	return lb
}

func loadVectors(t testing.TB) sim.Vectors {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v sim.Vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// play submits a stack run of the given taps for a player, with the clock
// moved forward by the run's length as it would be in real play.
func (e *env) playStack(pid, name string, taps []int) (int, scoreResp, string) {
	res, err := sim.ReplayStack(taps)
	if err != nil {
		e.t.Fatal(err)
	}
	s := e.session("stack", pid, name)
	e.clock.Advance(time.Duration(res.DurationMs) * time.Millisecond)
	return e.submit(s.Token, res.Score, taps)
}

func TestSubmitFlow(t *testing.T) {
	v := loadVectors(t)
	run := v.Stack[2] // a 40 point run that ends in a miss
	backends(t, func(t *testing.T, e *env) {
		code, got, errc := e.playStack("player-aaa", "Ana", run.Taps)
		if code != 200 {
			t.Fatalf("submit: %d %s", code, errc)
		}
		if got.Score != run.Want.Score || got.Best != run.Want.Score || got.Rank != 1 || !got.PersonalBest {
			t.Fatalf("got %+v", got)
		}
		lb := e.board("stack", "?player=player-aaa")
		if len(lb.Entries) != 1 || lb.Entries[0].Name != "Ana" || lb.Entries[0].Score != run.Want.Score {
			t.Fatalf("leaderboard %+v", lb)
		}
		if lb.You == nil || lb.You.Rank != 1 {
			t.Fatalf("you: %+v", lb.You)
		}
	})
}

func TestSessionCannotBeReused(t *testing.T) {
	backends(t, func(t *testing.T, e *env) {
		s := e.session("stack", "player-aaa", "Ana")
		e.clock.Advance(time.Second)
		if code, _, _ := e.submit(s.Token, 2, []int{625}); code != 200 {
			t.Fatalf("first: %d", code)
		}
		if code, _, errc := e.submit(s.Token, 2, []int{625}); code != 409 || errc != "already_submitted" {
			t.Fatalf("second: %d %s", code, errc)
		}
	})
}

func TestDatabaseBlocksReuseWhenCacheIsDown(t *testing.T) {
	m := store.NewMemory()
	e := newEnv(t, m, m)
	s := e.session("stack", "player-aaa", "Ana")
	e.clock.Advance(time.Second)
	e.submit(s.Token, 2, []int{625})
	m.FailNext = errors.New("redis down") // the claim check errors, so only the primary key protects us
	if code, _, errc := e.submit(s.Token, 2, []int{625}); code != 409 || errc != "already_submitted" {
		t.Fatalf("got %d %s", code, errc)
	}
}

func TestRejectsDishonestRuns(t *testing.T) {
	backends(t, func(t *testing.T, e *env) {
		// claims more than the inputs earn
		s := e.session("stack", "player-aaa", "Ana")
		e.clock.Advance(time.Second)
		if code, _, errc := e.submit(s.Token, 99, []int{625}); code != 422 || errc != "score_mismatch" {
			t.Fatalf("mismatch: %d %s", code, errc)
		}
		// a 15 second run submitted 1 second after the session started
		taps := loadVectors(t).Stack[0].Taps
		res, _ := sim.ReplayStack(taps)
		s = e.session("stack", "player-aaa", "Ana")
		e.clock.Advance(time.Second)
		if code, _, errc := e.submit(s.Token, res.Score, taps); code != 422 || errc != "too_fast" {
			t.Fatalf("too fast: %d %s", code, errc)
		}
		// machine speed taps
		if code, _, errc := e.submit(s.Token, 4, []int{625, 20}); code != 422 || errc != "bad_inputs" {
			t.Fatalf("inhuman: %d %s", code, errc)
		}
		// forged and expired tokens
		if code, _, errc := e.submit(s.Token+"x", 2, []int{625}); code != 401 || errc != "bad_token" {
			t.Fatalf("forged: %d %s", code, errc)
		}
		e.clock.Advance(20 * time.Minute)
		if code, _, errc := e.submit(s.Token, 2, []int{625}); code != 410 || errc != "expired" {
			t.Fatalf("expired: %d %s", code, errc)
		}
		if lb := e.board("stack", ""); len(lb.Entries) != 0 {
			t.Fatalf("rejected runs reached the leaderboard: %+v", lb.Entries)
		}
	})
}

func TestBestOnlyGoesUp(t *testing.T) {
	v := loadVectors(t)
	backends(t, func(t *testing.T, e *env) {
		e.playStack("player-aaa", "Ana", v.Stack[2].Taps) // 40
		code, got, _ := e.playStack("player-aaa", "Ana", v.Stack[4].Taps) // 5
		if code != 200 || got.Score != 5 || got.Best != 40 || got.PersonalBest {
			t.Fatalf("worse run: %d %+v", code, got)
		}
		lb := e.board("stack", "")
		if len(lb.Entries) != 1 || lb.Entries[0].Score != 40 {
			t.Fatalf("best was lowered: %+v", lb.Entries)
		}
	})
}

func TestRankingAcrossPlayers(t *testing.T) {
	v := loadVectors(t)
	backends(t, func(t *testing.T, e *env) {
		e.playStack("player-aaa", "Ana", v.Stack[3].Taps) // 13
		e.playStack("player-bbb", "Bo", v.Stack[2].Taps)  // 40
		_, got, _ := e.playStack("player-ccc", "Cy", v.Stack[4].Taps) // 5
		if got.Rank != 3 {
			t.Fatalf("rank %d", got.Rank)
		}
		lb := e.board("stack", "?limit=2&player=player-ccc")
		if len(lb.Entries) != 2 || lb.Entries[0].Name != "Bo" || lb.Entries[1].Name != "Ana" {
			t.Fatalf("top 2: %+v", lb.Entries)
		}
		if lb.You == nil || lb.You.Rank != 3 {
			t.Fatalf("you outside top 2: %+v", lb.You)
		}
		// boards are per game
		if lb := e.board("dotrush", ""); len(lb.Entries) != 0 {
			t.Fatalf("dotrush: %+v", lb.Entries)
		}
	})
}

func TestCacheRebuildsFromDatabase(t *testing.T) {
	v := loadVectors(t)
	backends(t, func(t *testing.T, e *env) {
		e.playStack("player-aaa", "Ana", v.Stack[3].Taps)
		e.playStack("player-bbb", "Bo", v.Stack[2].Taps)
		e.reset() // Redis restarted empty
		lb := e.board("stack", "")
		if lb.Source != "cache" || len(lb.Entries) != 2 || lb.Entries[0].Name != "Bo" {
			t.Fatalf("after rebuild: %+v", lb)
		}
	})
}

func TestServesFromDatabaseWhenCacheFails(t *testing.T) {
	v := loadVectors(t)
	m := store.NewMemory()
	e := newEnv(t, m, m)
	e.playStack("player-aaa", "Ana", v.Stack[2].Taps)
	m.FailNext = errors.New("redis down")
	lb := e.board("stack", "")
	if lb.Source != "database" || len(lb.Entries) != 1 {
		t.Fatalf("fallback: %+v", lb)
	}
}

func TestDotRushFlow(t *testing.T) {
	v := loadVectors(t)
	run := v.DotRush[0]
	backends(t, func(t *testing.T, e *env) {
		s := e.session("dotrush", "player-aaa", "Ana")
		// the vectors use a fixed seed; replay this session's seed instead by
		// tapping each of its dots after 400ms
		dots := sim.DotSequence(s.Seed, 60)
		var taps []sim.Tap
		for i, at := 0, 0; i < len(dots); i++ {
			at += 400
			if at >= sim.DotRoundMs {
				break
			}
			taps = append(taps, sim.Tap{T: at, X: dots[i].X, Y: dots[i].Y})
		}
		res, _ := sim.ReplayDotRush(s.Seed, taps)
		e.clock.Advance(31 * time.Second)
		code, got, errc := e.submit(s.Token, res.Score, taps)
		if code != 200 || got.Score != len(taps) {
			t.Fatalf("submit: %d %s %+v", code, errc, got)
		}
		// taps aimed at another seed's dots score differently, so claiming
		// that seed's score fails
		s = e.session("dotrush", "player-aaa", "Ana")
		e.clock.Advance(31 * time.Second)
		if s.Seed != run.Seed {
			if code, _, errc := e.submit(s.Token, run.Want.Score, run.Taps); code != 422 || errc != "score_mismatch" {
				t.Fatalf("wrong seed accepted: %d %s", code, errc)
			}
		}
	})
}

func TestRequestValidation(t *testing.T) {
	m := store.NewMemory()
	e := newEnv(t, m, m)
	cases := []struct {
		body any
		want string
	}{
		{sessionReq{Game: "snake", PlayerID: "player-aaa"}, "unknown_game"},
		{sessionReq{Game: "stack", PlayerID: "abc"}, "bad_player"},
		{sessionReq{Game: "stack", PlayerID: "has space!"}, "bad_player"},
		{map[string]any{"game": "stack", "playerId": "player-aaa", "admin": true}, "bad_json"},
	}
	for _, c := range cases {
		var eb errBody
		if code := e.do("POST", "/v1/sessions", c.body, &eb); code != 400 || eb.Code != c.want {
			t.Errorf("%+v: %d %s", c.body, code, eb.Code)
		}
	}
	var eb errBody
	if code := e.do("GET", "/v1/leaderboards/stack?limit=0", nil, &eb); code != 400 {
		t.Errorf("limit 0: %d", code)
	}
	if code := e.do("GET", "/v1/leaderboards/snake", nil, &eb); code != 404 {
		t.Errorf("unknown board: %d", code)
	}
	var s sessionResp
	e.do("POST", "/v1/sessions", sessionReq{Game: "stack", PlayerID: "player-aaa", Name: "   "}, &s)
	c, _ := e.srv.Signer.Verify(s.Token)
	if c.Name != "Player play" {
		t.Errorf("default name %q", c.Name)
	}
}

func TestCORS(t *testing.T) {
	m := store.NewMemory()
	e := newEnv(t, m, m)
	req, _ := http.NewRequest("OPTIONS", e.url+"/v1/sessions", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 204 || r.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("preflight: %d %q", r.StatusCode, r.Header.Get("Access-Control-Allow-Origin"))
	}
	req.Header.Set("Origin", "https://evil.example")
	r, _ = http.DefaultClient.Do(req)
	if r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origin allowed")
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"  Ana  ":                       "Ana",
		"a\u0000b​c":               "abc",
		"abcdefghijklmnopqrstuvwxyz":    "abcdefghijklmnopqrst",
		"Wàrung Bu Sri 🍜":              "Wàrung Bu Sri 🍜",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// BenchmarkSubmit measures a whole submission over HTTP: token check, replay,
// session claim, save and rank. It uses PostgreSQL and Redis when
// TEST_DATABASE_URL and TEST_REDIS_URL are set, else the in memory store.
func BenchmarkSubmit(b *testing.B) {
	ctx := context.Background()
	var d store.Durable
	var bd store.Board
	if dsn, rurl := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_REDIS_URL"); dsn != "" && rurl != "" {
		pg, err := store.OpenPostgres(ctx, dsn)
		if err != nil {
			b.Fatal(err)
		}
		defer pg.Close()
		rd, err := store.OpenRedis(ctx, rurl)
		if err != nil {
			b.Fatal(err)
		}
		defer rd.Close()
		d, bd = pg, rd
	} else {
		m := store.NewMemory()
		d, bd = m, m
	}
	e := newEnv(b, d, bd)
	taps := loadVectors(b).Stack[2].Taps
	res, _ := sim.ReplayStack(taps)
	raw, _ := json.Marshal(taps)
	toks := make([]string, b.N)
	for i := range toks {
		_, tok, _ := e.srv.Signer.Issue("stack", fmt.Sprintf("player-bench-%d", i%500), "Bench")
		toks[i] = tok
	}
	e.clock.Advance(time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body, _ := json.Marshal(scoreReq{Token: toks[i], ClaimedScore: res.Score, Taps: raw})
		r, err := http.Post(e.url+"/v1/scores", "application/json", bytes.NewReader(body))
		if err != nil || r.StatusCode != 200 {
			b.Fatalf("submit failed: %v", err)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
}
