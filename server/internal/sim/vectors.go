package sim

// Vectors is the shape of testdata/vectors.json, produced by cmd/genvectors
// and checked by both the Go and the TypeScript test suites.
type Vectors struct {
	Stack []struct {
		Name string      `json:"name"`
		Taps []int       `json:"taps"`
		Want StackResult `json:"want"`
	} `json:"stack"`
	DotRush []struct {
		Name string    `json:"name"`
		Seed uint32    `json:"seed"`
		Taps []Tap     `json:"taps"`
		Want DotResult `json:"want"`
	} `json:"dotrush"`
	Rng []struct {
		Seed  uint32   `json:"seed"`
		Draws []uint32 `json:"draws"`
	} `json:"rng"`
}
