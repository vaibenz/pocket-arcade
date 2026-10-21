// Package api is the HTTP layer: issue a session, accept a run, serve the
// leaderboard. Scores are never taken from the client. The server replays
// the submitted inputs and the replayed score is the one that counts.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/vaibenz/pocket-arcade/server/internal/session"
	"github.com/vaibenz/pocket-arcade/server/internal/sim"
	"github.com/vaibenz/pocket-arcade/server/internal/store"
)

const (
	maxBody = 64 << 10
	// Wall clock slack between a session being issued and its run being
	// submitted, to cover network latency and the countdown before play.
	clockSlack = 1500 * time.Millisecond
)

var (
	games     = map[string]bool{"stack": true, "dotrush": true}
	playerIDs = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)
)

type Server struct {
	Signer       *session.Signer
	Durable      store.Durable
	Board        store.Board
	Log          *slog.Logger
	AllowOrigins []string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("POST /v1/scores", s.submitScore)
	mux.HandleFunc("GET /v1/leaderboards/{game}", s.leaderboard)
	mux.HandleFunc("GET /healthz", s.health)
	return s.cors(mux)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad_json", "request body is not valid JSON for this endpoint")
		return false
	}
	return true
}

// CleanName trims a display name to at most 20 printable characters.
func CleanName(name string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(name) {
		if n == 20 {
			break
		}
		if unicode.IsControl(r) || r == '​' {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

type sessionReq struct {
	Game     string `json:"game"`
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
}

type sessionResp struct {
	SessionID   string `json:"sessionId"`
	Token       string `json:"token"`
	Seed        uint32 `json:"seed"`
	IssuedAt    int64  `json:"issuedAt"`
	ExpiresInMs int64  `json:"expiresInMs"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req sessionReq
	if !decode(w, r, &req) {
		return
	}
	if !games[req.Game] {
		fail(w, http.StatusBadRequest, "unknown_game", "unknown game")
		return
	}
	if !playerIDs.MatchString(req.PlayerID) {
		fail(w, http.StatusBadRequest, "bad_player", "playerId must be 6 to 64 letters, digits, _ or -")
		return
	}
	name := CleanName(req.Name)
	if name == "" {
		name = "Player " + req.PlayerID[:4]
	}
	c, tok, err := s.Signer.Issue(req.Game, req.PlayerID, name)
	if err != nil {
		s.Log.Error("issue session", "err", err)
		fail(w, http.StatusInternalServerError, "internal", "could not start a session")
		return
	}
	writeJSON(w, http.StatusCreated, sessionResp{
		SessionID: c.ID, Token: tok, Seed: c.Seed, IssuedAt: c.IssuedAt,
		ExpiresInMs: s.Signer.TTL().Milliseconds(),
	})
}

type scoreReq struct {
	Token        string          `json:"token"`
	ClaimedScore int             `json:"claimedScore"`
	Taps         json.RawMessage `json:"taps"`
}

type scoreResp struct {
	Score        int  `json:"score"`
	Best         int  `json:"best"`
	PersonalBest bool `json:"personalBest"`
	Rank         int  `json:"rank,omitempty"`
}

// replay runs the game's simulation over the submitted taps.
func replay(c session.Claims, raw json.RawMessage) (score, durationMs int, err error) {
	switch c.Game {
	case "stack":
		var taps []int
		if err := json.Unmarshal(raw, &taps); err != nil {
			return 0, 0, err
		}
		res, err := sim.ReplayStack(taps)
		return res.Score, res.DurationMs, err
	case "dotrush":
		var taps []sim.Tap
		if err := json.Unmarshal(raw, &taps); err != nil {
			return 0, 0, err
		}
		res, err := sim.ReplayDotRush(c.Seed, taps)
		return res.Score, res.DurationMs, err
	}
	return 0, 0, errors.New("unknown game")
}

func (s *Server) submitScore(w http.ResponseWriter, r *http.Request) {
	var req scoreReq
	if !decode(w, r, &req) {
		return
	}
	c, err := s.Signer.Verify(req.Token)
	switch {
	case errors.Is(err, session.ErrExpired):
		fail(w, http.StatusGone, "expired", "this session has expired")
		return
	case err != nil:
		fail(w, http.StatusUnauthorized, "bad_token", "invalid session token")
		return
	}

	score, durationMs, err := replay(c, req.Taps)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "bad_inputs", err.Error())
		return
	}
	if score != req.ClaimedScore {
		s.Log.Warn("score mismatch", "session", c.ID, "claimed", req.ClaimedScore, "replayed", score)
		fail(w, http.StatusUnprocessableEntity, "score_mismatch", "the submitted inputs do not produce the claimed score")
		return
	}
	if elapsed := s.Signer.Elapsed(c); elapsed+clockSlack < time.Duration(durationMs)*time.Millisecond {
		fail(w, http.StatusUnprocessableEntity, "too_fast", "the run is longer than the time since the session started")
		return
	}

	ctx := r.Context()
	// Redis rejects a replayed session cheaply; the runs primary key in
	// PostgreSQL is the backstop if Redis is unavailable.
	if ok, err := s.Board.ClaimSession(ctx, c.ID, s.Signer.TTL()); err != nil {
		s.Log.Warn("board claim failed, relying on database", "err", err)
	} else if !ok {
		fail(w, http.StatusConflict, "already_submitted", "this session already has a score")
		return
	}

	prev, best, err := s.Durable.SaveRun(ctx, store.Run{
		SessionID: c.ID, Game: c.Game, PlayerID: c.PlayerID, Name: c.Name,
		Score: score, DurationMs: durationMs, CreatedAt: time.Now().UTC(),
	})
	if errors.Is(err, store.ErrDuplicateSession) {
		fail(w, http.StatusConflict, "already_submitted", "this session already has a score")
		return
	}
	if err != nil {
		s.Log.Error("save run", "err", err)
		fail(w, http.StatusServiceUnavailable, "unavailable", "could not save the score, try again")
		return
	}

	resp := scoreResp{Score: score, Best: best, PersonalBest: score > prev}
	if err := s.Board.Offer(ctx, c.Game, c.PlayerID, c.Name, best); err != nil {
		s.Log.Warn("board offer failed", "err", err)
	} else if rank, ok, err := s.Board.Rank(ctx, c.Game, c.PlayerID); err == nil && ok {
		resp.Rank = rank
	}
	writeJSON(w, http.StatusOK, resp)
}

type leaderboardResp struct {
	Game    string        `json:"game"`
	Entries []store.Entry `json:"entries"`
	You     *store.Entry  `json:"you,omitempty"`
	Source  string        `json:"source"`
}

func (s *Server) leaderboard(w http.ResponseWriter, r *http.Request) {
	game := r.PathValue("game")
	if !games[game] {
		fail(w, http.StatusNotFound, "unknown_game", "unknown game")
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			fail(w, http.StatusBadRequest, "bad_limit", "limit must be 1 to 100")
			return
		}
		limit = n
	}
	ctx := r.Context()
	resp := leaderboardResp{Game: game, Entries: []store.Entry{}, Source: "cache"}

	entries, err := s.fromBoard(ctx, game, limit)
	if err != nil {
		s.Log.Warn("board read failed, serving from database", "err", err)
		resp.Source = "database"
		entries, err = s.Durable.TopBest(ctx, game, limit)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "unavailable", "leaderboard unavailable")
			return
		}
	}
	if entries != nil {
		resp.Entries = entries
	}

	if pid := r.URL.Query().Get("player"); pid != "" && resp.Source == "cache" {
		if rank, ok, err := s.Board.Rank(ctx, game, pid); err == nil && ok {
			for _, e := range entries {
				if e.PlayerID == pid {
					e := e
					resp.You = &e
				}
			}
			if resp.You == nil {
				resp.You = &store.Entry{Rank: rank, PlayerID: pid}
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// fromBoard reads the cached leaderboard, rebuilding it from PostgreSQL first
// if Redis has lost it.
func (s *Server) fromBoard(ctx context.Context, game string, limit int) ([]store.Entry, error) {
	ok, err := s.Board.Exists(ctx, game)
	if err != nil {
		return nil, err
	}
	if !ok {
		all, err := s.Durable.AllBest(ctx, game)
		if err != nil {
			return nil, err
		}
		if err := s.Board.Load(ctx, game, all); err != nil {
			return nil, err
		}
		s.Log.Info("rebuilt leaderboard cache", "game", game, "players", len(all))
	}
	return s.Board.Top(ctx, game, limit)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := map[string]string{"database": "ok", "cache": "ok"}
	code := http.StatusOK
	if err := s.Durable.Ping(ctx); err != nil {
		status["database"] = err.Error()
		code = http.StatusServiceUnavailable
	}
	if err := s.Board.Ping(ctx); err != nil {
		status["cache"] = err.Error() // degraded, still serving from the database
	}
	writeJSON(w, code, status)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, o := range s.AllowOrigins {
			if o == "*" || o == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				break
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
