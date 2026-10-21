package session

import (
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	s := NewSigner([]byte("k"), time.Minute)
	c, tok, err := s.Issue("stack", "p1", "Ana")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Verify(tok)
	if err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	s := NewSigner([]byte("k"), time.Minute)
	c, _, _ := s.Issue("stack", "p1", "Ana")
	c.Seed++ // try to pick a friendlier seed
	forged, _ := NewSigner([]byte("other"), time.Minute).Sign(c)
	if _, err := s.Verify(forged); err != ErrSignature {
		t.Fatalf("want ErrSignature, got %v", err)
	}
	_, tok, _ := s.Issue("stack", "p1", "Ana")
	p, sig, _ := strings.Cut(tok, ".")
	if _, err := s.Verify(p + "x." + sig); err != ErrSignature {
		t.Fatalf("edited payload: got %v", err)
	}
	for _, bad := range []string{"", ".", "abc", "abc."} {
		if _, err := s.Verify(bad); err != ErrMalformed {
			t.Errorf("%q: got %v", bad, err)
		}
	}
}

func TestExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := NewSigner([]byte("k"), time.Minute).WithClock(func() time.Time { return now })
	_, tok, _ := s.Issue("dotrush", "p", "n")
	now = now.Add(2 * time.Minute)
	if _, err := s.Verify(tok); err != ErrExpired {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}
