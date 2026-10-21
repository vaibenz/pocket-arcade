package sim

import "sort"

// Dot Rush: one dot at a time appears at a seeded position. Tap it before it
// fades. Each dot lives a little less long than the last, and the round ends
// after 30 seconds.
const (
	DotArenaW      = 360
	DotArenaH      = 640
	DotRadius      = 36
	DotRoundMs     = 30000
	DotMaxTaps     = 400
	DotMinTapGapMs = 50
)

type Tap struct {
	T int `json:"t"`
	X int `json:"x"`
	Y int `json:"y"`
}

// Rng is mulberry32. It is small, fast and easy to reproduce exactly in
// JavaScript with Math.imul and unsigned shifts.
type Rng struct{ a uint32 }

func NewRng(seed uint32) *Rng { return &Rng{a: seed} }

func (r *Rng) Next() uint32 {
	r.a += 0x6D2B79F5
	t := (r.a ^ r.a>>15) * (1 | r.a)
	t = (t + (t^t>>7)*(61|t)) ^ t
	return t ^ t>>14
}

func DotLifetime(i int) int {
	return max(450, 1200-25*i)
}

type Dot struct{ X, Y int }

// DotSequence returns the first n dot positions for a seed.
func DotSequence(seed uint32, n int) []Dot {
	rng := NewRng(seed)
	out := make([]Dot, n)
	for i := range out {
		out[i] = nextDot(rng)
	}
	return out
}

func nextDot(rng *Rng) Dot {
	x := DotRadius + int(rng.Next()%uint32(DotArenaW-2*DotRadius))
	y := DotRadius + int(rng.Next()%uint32(DotArenaH-2*DotRadius))
	return Dot{x, y}
}

type DotResult struct {
	Score      int `json:"score"`
	Misses     int `json:"misses"`
	Expired    int `json:"expired"`
	DurationMs int `json:"durationMs"`
}

// ReplayDotRush scores a round from the player's taps, with t in ms since the
// round started.
func ReplayDotRush(seed uint32, taps []Tap) (DotResult, error) {
	if len(taps) > DotMaxTaps {
		return DotResult{}, ErrTooManyInputs
	}
	if !sort.SliceIsSorted(taps, func(i, j int) bool { return taps[i].T < taps[j].T }) {
		return DotResult{}, ErrInputTiming
	}
	rng := NewRng(seed)
	var r DotResult
	i, start, dot := 0, 0, nextDot(rng)
	last := -DotMinTapGapMs
	for _, tp := range taps {
		if tp.T < 0 || tp.T >= DotRoundMs || tp.T-last < DotMinTapGapMs {
			return DotResult{}, ErrInputTiming
		}
		last = tp.T
		for tp.T >= start+DotLifetime(i) {
			start += DotLifetime(i)
			i++
			r.Expired++
			dot = nextDot(rng)
		}
		dx, dy := tp.X-dot.X, tp.Y-dot.Y
		if dx*dx+dy*dy <= DotRadius*DotRadius {
			r.Score++
			start, i = tp.T, i+1
			dot = nextDot(rng)
		} else {
			r.Misses++
		}
	}
	r.DurationMs = DotRoundMs
	return r, nil
}
