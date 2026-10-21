package sim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStackPositionPingPongs(t *testing.T) {
	// width 200 leaves 200 units of travel; speed 160 u/s for block 0
	cases := map[int]int{0: 0, 500: 80, 1250: 200, 1500: 160, 2500: 0, 2600: 16}
	for dt, want := range cases {
		if got := StackPosition(0, 200, dt); got != want {
			t.Errorf("dt=%d: got %d want %d", dt, got, want)
		}
	}
	if StackPosition(0, StackBoardWidth, 999) != 0 {
		t.Error("full width block should not move")
	}
}

func TestStackSpeedCaps(t *testing.T) {
	if StackSpeed(0) != 160 || StackSpeed(1000) != 480 {
		t.Fatal("unexpected speed curve")
	}
}

func TestReplayStackPerfectKeepsWidth(t *testing.T) {
	// x = 100 at dt = 625ms for block 0 (160 u/s)
	r, err := ReplayStack([]int{625})
	if err != nil {
		t.Fatal(err)
	}
	if r.Score != 2 || r.Perfects != 1 || r.FinalWidth != 200 {
		t.Fatalf("got %+v", r)
	}
}

func TestReplayStackTrimsAndEnds(t *testing.T) {
	// block 0 at dt 750 lands at x=120: overlap 180 wide
	r, _ := ReplayStack([]int{750})
	if r.Score != 1 || r.FinalWidth != 180 {
		t.Fatalf("trim: got %+v", r)
	}
	// block 0 at dt 1250 lands at x=200, leaving a 100 wide block at 200;
	// block 1 at dt 120 is at x=20 and misses it completely
	r, _ = ReplayStack([]int{1250, 120, 625})
	if !r.GameOver || r.Placed != 1 {
		t.Fatalf("miss: got %+v", r)
	}
}

func TestReplayStackRejectsInhumanTiming(t *testing.T) {
	if _, err := ReplayStack([]int{625, 30}); err != ErrInputTiming {
		t.Fatalf("want ErrInputTiming, got %v", err)
	}
	if _, err := ReplayStack(make([]int, StackMaxBlocks+1)); err != ErrTooManyInputs {
		t.Fatalf("want ErrTooManyInputs, got %v", err)
	}
}

func TestRngMatchesReference(t *testing.T) {
	// reference values from the JavaScript mulberry32 implementation
	rng := NewRng(1)
	want := []uint32{2693262067, 11749833, 2265367787}
	for i, w := range want {
		if got := rng.Next(); got != w {
			t.Fatalf("draw %d: got %d want %d", i, got, w)
		}
	}
}

func TestDotRushHitsAndExpiry(t *testing.T) {
	dots := DotSequence(42, 3)
	taps := []Tap{
		{T: 300, X: dots[0].X, Y: dots[0].Y},                   // hit dot 0
		{T: 400, X: dots[1].X + DotRadius + 1, Y: dots[1].Y},   // just outside dot 1
		{T: 300 + 1175 + 10, X: dots[2].X, Y: dots[2].Y},       // dot 1 expired, hit dot 2
	}
	r, err := ReplayDotRush(42, taps)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score != 2 || r.Misses != 1 || r.Expired != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestDotRushRejectsBadInput(t *testing.T) {
	if _, err := ReplayDotRush(1, []Tap{{T: 500}, {T: 100}}); err != ErrInputTiming {
		t.Fatal("unsorted taps accepted")
	}
	if _, err := ReplayDotRush(1, []Tap{{T: 100}, {T: 120}}); err != ErrInputTiming {
		t.Fatal("machine speed taps accepted")
	}
	if _, err := ReplayDotRush(1, []Tap{{T: DotRoundMs}}); err != ErrInputTiming {
		t.Fatal("tap after the round accepted")
	}
}

// The web client runs the same simulation to draw the game. Both suites
// check these vectors so client and server can never drift apart.
func TestSharedVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v Vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Stack {
		got, _ := ReplayStack(c.Taps)
		if got != c.Want {
			t.Errorf("stack %s: got %+v want %+v", c.Name, got, c.Want)
		}
	}
	for _, c := range v.DotRush {
		got, _ := ReplayDotRush(c.Seed, c.Taps)
		if got != c.Want {
			t.Errorf("dotrush %s: got %+v want %+v", c.Name, got, c.Want)
		}
	}
	for _, c := range v.Rng {
		rng := NewRng(c.Seed)
		for i, w := range c.Draws {
			if g := rng.Next(); g != w {
				t.Errorf("rng seed %d draw %d: got %d want %d", c.Seed, i, g, w)
			}
		}
	}
}

func BenchmarkReplayStack(b *testing.B) {
	raw, _ := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vectors.json"))
	var v Vectors
	_ = json.Unmarshal(raw, &v)
	taps := v.Stack[0].Taps // a full 60 block run
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ReplayStack(taps)
	}
}

func BenchmarkReplayDotRush(b *testing.B) {
	raw, _ := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vectors.json"))
	var v Vectors
	_ = json.Unmarshal(raw, &v)
	c := v.DotRush[0] // a full 30 second round
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ReplayDotRush(c.Seed, c.Taps)
	}
}
