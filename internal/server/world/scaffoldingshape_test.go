package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestScaffoldingBoxesFollowTheBody pins the body-relative collision: the
// stable shape above the cell, the bottom plate for a body above an unstable
// bottom piece, nothing otherwise.
// vanilla: ScaffoldingBlock.getCollisionShape
func TestScaffoldingBoxesFollowTheBody(t *testing.T) {
	t.Parallel()
	state := func(distance, bottom string) uint16 {
		s, ok := registry.FindState("minecraft:scaffolding", map[string]string{
			"bottom": bottom, "distance": distance, "waterlogged": "false"})
		if !ok {
			t.Fatal("no scaffolding state")
		}
		return s
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	if _, ok := ScaffoldingBoxes(stone, 10, 11); ok {
		t.Error("stone reported as scaffolding")
	}
	for _, tc := range []struct {
		name   string
		state  uint16
		bottom float64
		want   int
	}{
		{"above", state("0", "false"), 11, 5},
		{"inside", state("0", "false"), 10.5, 0},
		{"unstable bottom, inside", state("1", "true"), 10.5, 1},
		{"unstable bottom, at the plate", state("1", "true"), 10, 1},
		{"unstable bottom, below the cell", state("1", "true"), 9.5, 0},
		{"stable bottom flag, inside", state("0", "true"), 10.5, 0},
	} {
		got, ok := ScaffoldingBoxes(tc.state, 10, tc.bottom)
		if !ok || len(got) != tc.want {
			t.Errorf("%s: %d boxes (ok %v), want %d", tc.name, len(got), ok, tc.want)
		}
	}
}
