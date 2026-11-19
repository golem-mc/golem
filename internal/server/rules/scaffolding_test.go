package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The extension walk as ordinary unit tests, on a world that is nothing but
// the blocks each case sets: the direction each walk takes, the pieces it
// steps over, the cells it refuses. The black-box tests in
// internal/server/*_test.go hold the placement around these decisions, and the
// pieces these cases stand in the walk's way carry whatever properties: the
// walk asks the block's name alone.

// TestScaffoldingExtendToWalksTheClicksVanillaMakes checks the walk's
// direction and its reading of the cells along the way.
// vanilla: ScaffoldingBlockItem.updatePlacementContext
func TestScaffoldingExtendToWalksTheClicksVanillaMakes(t *testing.T) {
	scaffold := mustState(t, "minecraft:scaffolding", nil)
	stone := defaultState(t, "minecraft:stone")
	grass := defaultState(t, "minecraft:short_grass")
	bounds := []int32{-8, 8}
	cases := []struct {
		what string
		ctx  PlacementContext
		set  func(w testWorld)
		want [3]int32
		ok   bool
	}{
		// Any click but a top one walks the column up, whatever faces it.
		{"a side click walks up",
			PlacementContext{Face: protocol.FaceNorth},
			func(w testWorld) { w.set(0, 1, 0, scaffold); w.set(0, 2, 0, scaffold) },
			[3]int32{0, 3, 0}, true},
		{"a bottom click walks up as well",
			PlacementContext{Face: protocol.FaceBottom},
			func(w testWorld) {},
			[3]int32{0, 1, 0}, true},
		// A top click walks out along the player's look.
		{"a top click looking south walks south",
			PlacementContext{Face: protocol.FaceTop},
			func(w testWorld) {},
			[3]int32{0, 0, 1}, true},
		{"a top click looking west walks west",
			PlacementContext{Face: protocol.FaceTop, Yaw: 90},
			func(w testWorld) {},
			[3]int32{-1, 0, 0}, true},
		{"a top click steps over the pieces in its way",
			PlacementContext{Face: protocol.FaceTop},
			func(w testWorld) { w.set(0, 0, 1, scaffold); w.set(0, 0, 2, scaffold) },
			[3]int32{0, 0, 3}, true},
		{"a top click takes a replaceable cell",
			PlacementContext{Face: protocol.FaceTop, Yaw: 180},
			func(w testWorld) { w.set(0, 0, -1, grass) },
			[3]int32{0, 0, -1}, true},
		{"a walk that meets stone places nothing",
			PlacementContext{Face: protocol.FaceTop},
			func(w testWorld) { w.set(0, 0, 1, scaffold); w.set(0, 0, 2, stone) },
			[3]int32{}, false},
		// Sneaking walks the clicked face, out its far side for a click
		// traced from inside the block.
		{"sneaking walks the clicked face",
			PlacementContext{Face: protocol.FaceEast, Sneaking: true},
			func(w testWorld) {},
			[3]int32{1, 0, 0}, true},
		{"sneaking walks the clicked face over the pieces beside",
			PlacementContext{Face: protocol.FaceEast, Sneaking: true},
			func(w testWorld) { w.set(1, 0, 0, scaffold); w.set(2, 0, 0, scaffold) },
			[3]int32{3, 0, 0}, true},
		{"a sneaking top click walks up",
			PlacementContext{Face: protocol.FaceTop, Sneaking: true},
			func(w testWorld) {},
			[3]int32{0, 1, 0}, true},
		{"a sneaking inside click walks out the far side",
			PlacementContext{Face: protocol.FaceEast, Sneaking: true, Inside: true},
			func(w testWorld) {},
			[3]int32{-1, 0, 0}, true},
	}
	for _, c := range cases {
		w := testWorld{}
		c.set(w)
		x, y, z, ok, tooHigh := ScaffoldingExtendTo(w, 0, 0, 0, c.ctx, bounds[0], bounds[1])
		if ok != c.ok || tooHigh {
			t.Errorf("%s: ok=%v tooHigh=%v, want ok=%v and no build limit",
				c.what, ok, tooHigh, c.ok)
			continue
		}
		if ok && (x != c.want[0] || y != c.want[1] || z != c.want[2]) {
			t.Errorf("%s: walked to (%d,%d,%d), want (%d,%d,%d)",
				c.what, x, y, z, c.want[0], c.want[1], c.want[2])
		}
	}
}
