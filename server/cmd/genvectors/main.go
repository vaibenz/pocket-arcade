// genvectors writes testdata/vectors.json: seeded runs replayed by the Go
// simulation, which the web client's tests must reproduce exactly.
//
//	go run ./cmd/genvectors > ../testdata/vectors.json
package main

import (
	"encoding/json"
	"math/rand"
	"os"

	"github.com/vaibenz/pocket-arcade/server/internal/sim"
)

type stackCase struct {
	Name string          `json:"name"`
	Taps []int           `json:"taps"`
	Want sim.StackResult `json:"want"`
}

type dotCase struct {
	Name string        `json:"name"`
	Seed uint32        `json:"seed"`
	Taps []sim.Tap     `json:"taps"`
	Want sim.DotResult `json:"want"`
}

type rngCase struct {
	Seed  uint32   `json:"seed"`
	Draws []uint32 `json:"draws"`
}

func main() {
	r := rand.New(rand.NewSource(7))
	out := struct {
		Stack   []stackCase `json:"stack"`
		DotRush []dotCase   `json:"dotrush"`
		Rng     []rngCase   `json:"rng"`
	}{}

	for k, skill := range []int{0, 4, 12, 30, 80} {
		taps := stackBot(r, skill)
		res, _ := sim.ReplayStack(taps)
		out.Stack = append(out.Stack, stackCase{Name: name("aim", k, skill), Taps: taps, Want: res})
	}
	for k := 0; k < 3; k++ {
		taps := make([]int, 12)
		for i := range taps {
			taps[i] = 120 + r.Intn(4000)
		}
		res, _ := sim.ReplayStack(taps)
		out.Stack = append(out.Stack, stackCase{Name: name("random", k, 0), Taps: taps, Want: res})
	}

	for k, skill := range []int{2, 10, 40} {
		seed := r.Uint32()
		taps := dotBot(r, seed, skill)
		res, _ := sim.ReplayDotRush(seed, taps)
		out.DotRush = append(out.DotRush, dotCase{Name: name("bot", k, skill), Seed: seed, Taps: taps, Want: res})
	}

	for _, seed := range []uint32{0, 1, 42, 0xFFFFFFFF, r.Uint32()} {
		rng := sim.NewRng(seed)
		c := rngCase{Seed: seed}
		for i := 0; i < 8; i++ {
			c.Draws = append(c.Draws, rng.Next())
		}
		out.Rng = append(out.Rng, c)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func name(prefix string, k, skill int) string {
	b, _ := json.Marshal(map[string]int{"run": k, "jitter": skill})
	return prefix + string(b)
}

// stackBot taps when the block is within jitter units of the tower edge.
func stackBot(r *rand.Rand, jitter int) []int {
	px, pw := sim.StackStartX, sim.StackStartWidth
	var taps []int
	for i := 0; i < 60; i++ {
		target := px + r.Intn(2*jitter+1) - jitter
		best, bestDist := sim.StackMinTapMs, 1<<30
		for dt := sim.StackMinTapMs; dt < 4000; dt++ {
			if d := abs(sim.StackPosition(i, pw, dt) - target); d < bestDist {
				best, bestDist = dt, d
			}
		}
		taps = append(taps, best)
		res, _ := sim.ReplayStack(taps)
		if res.GameOver {
			break
		}
		// rebuild tower state from the replay so the bot aims at the real edge
		px, pw = towerAfter(taps)
	}
	return taps
}

func towerAfter(taps []int) (int, int) {
	px, pw := sim.StackStartX, sim.StackStartWidth
	for i, dt := range taps {
		x := sim.StackPosition(i, pw, dt)
		left, right := max(x, px), min(x+pw, px+pw)
		if abs(x-px) > sim.StackPerfectSlack {
			px, pw = left, right-left
		}
	}
	return px, pw
}

// dotBot taps every 250 to 900 ms, aiming within jitter px of the live dot.
func dotBot(r *rand.Rand, seed uint32, jitter int) []sim.Tap {
	dots := sim.DotSequence(seed, 400)
	var taps []sim.Tap
	t, i, start := 0, 0, 0
	for {
		t += 250 + r.Intn(650)
		if t >= sim.DotRoundMs {
			return taps
		}
		for t >= start+sim.DotLifetime(i) {
			start += sim.DotLifetime(i)
			i++
		}
		tap := sim.Tap{
			T: t,
			X: dots[i].X + r.Intn(2*jitter+1) - jitter,
			Y: dots[i].Y + r.Intn(2*jitter+1) - jitter,
		}
		taps = append(taps, tap)
		dx, dy := tap.X-dots[i].X, tap.Y-dots[i].Y
		if dx*dx+dy*dy <= sim.DotRadius*sim.DotRadius {
			start, i = t, i+1
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
