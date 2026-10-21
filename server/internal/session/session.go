// Package session issues and verifies signed play sessions. A session fixes
// the game, the player and the seed before play starts, so a score can only
// be submitted for a run the server actually handed out.
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrMalformed = errors.New("malformed token")
	ErrSignature = errors.New("bad token signature")
	ErrExpired   = errors.New("session expired")
)

type Claims struct {
	ID       string `json:"sid"`
	Game     string `json:"game"`
	PlayerID string `json:"pid"`
	Name     string `json:"name"`
	Seed     uint32 `json:"seed"`
	IssuedAt int64  `json:"iat"` // unix ms
}

type Signer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

func NewSigner(key []byte, ttl time.Duration) *Signer {
	return &Signer{key: key, ttl: ttl, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Signer) WithClock(now func() time.Time) *Signer {
	s.now = now
	return s
}

func (s *Signer) TTL() time.Duration { return s.ttl }

// Issue creates a session with a random id and seed.
func (s *Signer) Issue(game, playerID, name string) (Claims, string, error) {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Claims{}, "", err
	}
	c := Claims{
		ID:       hex.EncodeToString(b[:16]),
		Game:     game,
		PlayerID: playerID,
		Name:     name,
		Seed:     binary.BigEndian.Uint32(b[16:]),
		IssuedAt: s.now().UnixMilli(),
	}
	tok, err := s.Sign(c)
	return c, tok, err
}

func (s *Signer) Sign(c Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	return p + "." + s.mac(p), nil
}

func (s *Signer) Verify(token string) (Claims, error) {
	p, sig, ok := strings.Cut(token, ".")
	if !ok || p == "" || sig == "" {
		return Claims{}, ErrMalformed
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(p))) {
		return Claims{}, ErrSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrMalformed
	}
	if s.now().Sub(time.UnixMilli(c.IssuedAt)) > s.ttl {
		return Claims{}, ErrExpired
	}
	return c, nil
}

// Elapsed is how long ago the session was issued.
func (s *Signer) Elapsed(c Claims) time.Duration {
	return s.now().Sub(time.UnixMilli(c.IssuedAt))
}

func (s *Signer) mac(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
