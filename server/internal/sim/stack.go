// Package sim replays game inputs on the server so a score is computed,
// not trusted. Every rule here has a twin in web/src/sim, and both sides
// are checked against the same vectors in testdata/vectors.json.
//
// All arithmetic is integer so Go and JavaScript produce identical results.
package sim

import "errors"

// Stack: a block slides back and forth above the tower. The player taps to
// drop it. Whatever overhangs the block below is cut off, so the tower
// narrows until a block misses completely.
const (
	StackBoardWidth   = 400
	StackStartX       = 100
	StackStartWidth   = 200
	StackPerfectSlack = 4
	StackMinTapMs     = 120
	StackMaxTapMs     = 20000
	StackMaxBlocks    = 500
)

var (
	ErrTooManyInputs = errors.New("too many inputs")
	ErrInputTiming   = errors.New("input timing outside human range")
)

// StackSpeed is the slide speed in board units per second for the block
// placed at index i (0 is the first block dropped on the base).
func StackSpeed(i int) int {
	s := 160 + 14*i
	if s > 480 {
		s = 480
	}
	return s
}

// StackPosition is the left edge of a sliding block of width w, dt ms after
// it spawned at x = 0 moving right.
func StackPosition(i, w, dtMs int) int {
	travel := StackBoardWidth - w
	if travel <= 0 {
		return 0
	}
	d := (StackSpeed(i) * dtMs / 1000) % (2 * travel)
	if d <= travel {
		return d
	}
	return 2*travel - d
}

type StackResult struct {
	Score      int  `json:"score"`
	Placed     int  `json:"placed"`
	Perfects   int  `json:"perfects"`
	GameOver   bool `json:"gameOver"`
	DurationMs int  `json:"durationMs"`
	FinalWidth int  `json:"finalWidth"`
}

// ReplayStack scores a run from the time in ms between each block spawning
// and the player's tap. Inputs after the losing tap are ignored.
func ReplayStack(taps []int) (StackResult, error) {
	if len(taps) > StackMaxBlocks {
		return StackResult{}, ErrTooManyInputs
	}
	px, pw := StackStartX, StackStartWidth
	var r StackResult
	for i, dt := range taps {
		if dt < StackMinTapMs || dt > StackMaxTapMs {
			return StackResult{}, ErrInputTiming
		}
		r.DurationMs += dt
		x := StackPosition(i, pw, dt)
		left, right := max(x, px), min(x+pw, px+pw)
		if right-left <= 0 {
			r.GameOver = true
			break
		}
		if abs(x-px) <= StackPerfectSlack {
			r.Score += 2
			r.Perfects++
		} else {
			r.Score++
			px, pw = left, right-left
		}
		r.Placed++
	}
	r.FinalWidth = pw
	return r, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
