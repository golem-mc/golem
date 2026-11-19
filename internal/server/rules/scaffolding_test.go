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

// TestScaffoldingExtendToWalksOffTheWorld checks the ends of the walk: a
// horizontal one reaches seven cells past the clicked piece and no further,
// and a vertical one runs to the world's bounds — off the top with the
// build-limit answer, off the bottom refusing quietly.
// vanilla: ScaffoldingBlockItem.updatePlacementContext
func TestScaffoldingExtendToWalksOffTheWorld(t *testing.T) {
	scaffold := mustState(t, "minecraft:scaffolding", nil)
	// Seven pieces stand south of the clicked one: the walk reads a cell
	// per step and runs out of the seven before the free eighth.
	seven := testWorld{}
	for i := int32(1); i <= 7; i++ {
		seven.set(0, 0, i, scaffold)
	}
	if _, _, _, ok, tooHigh := ScaffoldingExtendTo(seven, 0, 0, 0,
		PlacementContext{Face: protocol.FaceTop}, -8, 8); ok || tooHigh {
		t.Errorf("seven pieces: ok=%v tooHigh=%v, want the walk out of steps", ok, tooHigh)
	}
	// Six leave the seventh cell free for the walk to take.
	six := testWorld{}
	for i := int32(1); i <= 6; i++ {
		six.set(0, 0, i, scaffold)
	}
	x, y, z, ok, tooHigh := ScaffoldingExtendTo(six, 0, 0, 0,
		PlacementContext{Face: protocol.FaceTop}, -8, 8)
	if !ok || tooHigh || x != 0 || y != 0 || z != 7 {
		t.Errorf("six pieces: (%d,%d,%d) ok=%v tooHigh=%v, want (0,0,7) taken", x, y, z, ok, tooHigh)
	}
	// A vertical walk runs to the world's own bounds, which the caller
	// hands it: the first cell past the top answers tooHigh for the action
	// bar line, the first past the bottom refuses with nothing said.
	if _, _, _, ok, tooHigh := ScaffoldingExtendTo(testWorld{}, 0, 0, 0,
		PlacementContext{Face: protocol.FaceNorth}, -8, 8); !ok || tooHigh {
		t.Errorf("a walk inside the world: ok=%v tooHigh=%v, want the first cell taken", ok, tooHigh)
	}
	up := testWorld{}
	up.set(0, 1, 0, scaffold)
	if _, _, _, ok, tooHigh := ScaffoldingExtendTo(up, 0, 0, 0,
		PlacementContext{Face: protocol.FaceNorth}, -8, 0); ok || !tooHigh {
		t.Errorf("a walk off the top: ok=%v tooHigh=%v, want tooHigh at the end", ok, tooHigh)
	}
	if _, _, _, ok, tooHigh := ScaffoldingExtendTo(testWorld{}, 0, 0, 0,
		PlacementContext{Face: protocol.FaceBottom, Sneaking: true}, 0, 8); ok || tooHigh {
		t.Errorf("a walk off the bottom: ok=%v tooHigh=%v, want a quiet refusal", ok, tooHigh)
	}
}
