package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The look ray the uses ride: the walk's cells and faces, the crossing
// test's fractions, and the stops the outlines and the fluid shapes make.

// newRayWorld loads a flat world with the chunks around the origin held,
// the way a server with players there keeps them, so a clip can walk the
// cells it crosses. The ground is stone up to y=63 and air from
// GroundLevel up, which is what the rays here are aimed at.
func newRayWorld(t *testing.T) *World {
	t.Helper()
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w
}

// TestTraverseBlocksWalksTheCells: the walk visits the start cell first,
// then one cell per boundary crossed, each carrying the face the ray came
// in through, and stops the moment a visit asks it to.
func TestTraverseBlocksWalksTheCells(t *testing.T) {
	t.Parallel()

	// The straight walk east: one face, the west one, every step.
	var got []string
	TraverseBlocks(0.5, 0.5, 0.5, 3, 0, 0, func(x, y, z, face int32) bool {
		got = append(got, strconvCell(x, y, z)+" f"+strconvFace(face))
		return false
	})
	want := []string{
		"0,0,0 f4", "1,0,0 f4", "2,0,0 f4", "3,0,0 f4",
	}
	if !equalStrings(got, want) {
		t.Errorf("the east walk visited %v, want %v", got, want)
	}

	// The diagonal walk, its ties broken z before y before x, the order
	// vanilla's nested comparisons step in.
	got = nil
	TraverseBlocks(0.5, 0.5, 0.5, 2, 2, 0, func(x, y, z, face int32) bool {
		got = append(got, strconvCell(x, y, z)+" f"+strconvFace(face))
		return false
	})
	want = []string{
		"0,0,0 f0", "0,1,0 f0", "1,1,0 f4", "1,2,0 f0", "2,2,0 f4",
	}
	if !equalStrings(got, want) {
		t.Errorf("the diagonal walk visited %v, want %v", got, want)
	}

	// A visit that answers stop ends the walk there.
	got = nil
	TraverseBlocks(0.5, 0.5, 0.5, 3, 0, 0, func(x, y, z, face int32) bool {
		got = append(got, strconvCell(x, y, z))
		return len(got) == 2
	})
	if !equalStrings(got, []string{"0,0,0", "1,0,0"}) {
		t.Errorf("the stopped walk visited %v, want two cells", got)
	}

	// A segment of no length visits the start cell alone.
	got = nil
	TraverseBlocks(0.5, 0.5, 0.5, 0, 0, 0, func(x, y, z, face int32) bool {
		got = append(got, strconvCell(x, y, z))
		return false
	})
	if !equalStrings(got, []string{"0,0,0"}) {
		t.Errorf("the still walk visited %v, want the start cell alone", got)
	}
}

// TestSegmentAABBEntersTheBox: the crossing test answers the fraction the
// segment has run when it first enters, the face of the box that crossing
// came through, zero with the run-into face for a start already inside,
// and no entry for a ray that passes the box by.
func TestSegmentAABBEntersTheBox(t *testing.T) {
	t.Parallel()

	tenter, face, ok := SegmentAABB(0.5, 0.5, 0.5, 2, 0, 0, 1, 0, 0, 2, 1, 1)
	if !ok || tenter != 0.25 || face != protocol.FaceWest {
		t.Errorf("the crossing from the west = (%f, f%d, %v), want (0.25, west, true)", tenter, face, ok)
	}
	tenter, face, ok = SegmentAABB(1.5, 0.5, 0.5, 0, -2, 0, 1, 0, 0, 2, 1, 1)
	if !ok || tenter != 0 || face != protocol.FaceTop {
		t.Errorf("the crossing from inside = (%f, f%d, %v), want (0, top, true)", tenter, face, ok)
	}
	if _, _, ok := SegmentAABB(0.5, 0.5, 0.5, -1, 0, 0, 1, 0, 0, 2, 1, 1); ok {
		t.Error("the ray running away from the box entered it")
	}
	// The diagonal that reaches the box's y bottom past its x far side
	// slips under the corner.
	if _, _, ok := SegmentAABB(0.5, 0.5, 0.5, 2, 0.6, 0, 1, 1, 0, 2, 2, 1); ok {
		t.Error("the ray that slips under the corner entered the box")
	}
	tenter, face, ok = SegmentAABB(0.5, 0.5, 0.5, 2, 1.2, 0, 1, 1, 0, 2, 2, 1)
	if !ok {
		t.Fatal("the ray that rounds the corner missed the box")
	}
	if tenter < 0.4166 || tenter > 0.4168 || face != protocol.FaceBottom {
		t.Errorf("the corner-rounding crossing = (%f, f%d), want (5/12, bottom)", tenter, face)
	}
}

// TestClipBlocksStopsAtTheShapes: the clip stops on the outlines and on
// the fluid shape the rule names, at the nearer of the two a cell carries,
// and never on the fluids the rule leaves unnamed.
func TestClipBlocksStopsAtTheShapes(t *testing.T) {
	w := newRayWorld(t)
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	source := func(state uint16) bool { return state == water }

	// Straight down onto the flat ground: the stone's top face, six
	// blocks of the ten the segment runs.
	hit, ok := w.ClipBlocks(2.5, 70, 4.5, 0, -10, 0, nil)
	if !ok || hit.X != 2 || hit.Y != 63 || hit.Z != 4 ||
		hit.Face != protocol.FaceTop || hit.T != 0.6 {
		t.Errorf("the ground clip = %+v (%v), want (2,63,4) top at t=0.6", hit, ok)
	}

	// The source the ray dips into: the fluid shape's surface, not the
	// whole cell.
	if !w.SetBlock(2, 64, 4, water) {
		t.Fatal("the world did not take the water")
	}
	hit, ok = w.ClipBlocks(2.5, 70, 4.5, 0, -10, 0, source)
	if !ok || hit.X != 2 || hit.Y != 64 || hit.Z != 4 || !hit.Fluid {
		t.Fatalf("the water clip = %+v (%v), want the water cell, a fluid stop", hit, ok)
	}
	if hit.T < 0.511 || hit.T > 0.512 {
		t.Errorf("the water clip's t = %f, want the eight-ninths surface at 0.511", hit.T)
	}

	// The same ray with the fluids unnamed pours past the water onto the
	// stone under it.
	hit, ok = w.ClipBlocks(2.5, 70, 4.5, 0, -10, 0, nil)
	if !ok || hit.X != 2 || hit.Y != 63 || hit.Z != 4 || hit.Fluid {
		t.Errorf("the pour clip = %+v (%v), want the stone under the water", hit, ok)
	}

	// The ray that crosses the source cell over the surface stops on
	// nothing: the ninth it passes over is not the water.
	hit, ok = w.ClipBlocks(2.2, 64.95, 4.5, 2, -0.1, 0, source)
	if ok {
		t.Errorf("the grazing clip stopped at %+v, want no stop", hit)
	}

	// The ray that starts inside the water stops at once, in its own
	// cell.
	hit, ok = w.ClipBlocks(2.5, 64.5, 4.5, 0, -1, 0, source)
	if !ok || hit.X != 2 || hit.Y != 64 || hit.Z != 4 || hit.T != 0 {
		t.Errorf("the submerged clip = %+v (%v), want the eye's own cell at t=0", hit, ok)
	}

	// The cobweb above the water holds the ray on its outline.
	web, ok := registry.FindState("minecraft:cobweb", nil)
	if !ok {
		t.Fatal("the block registry has no cobweb")
	}
	if !w.SetBlock(2, 65, 4, web) {
		t.Fatal("the world did not take the web")
	}
	hit, ok = w.ClipBlocks(2.5, 70, 4.5, 0, -10, 0, source)
	if !ok || hit.X != 2 || hit.Y != 65 || hit.Z != 4 || hit.Fluid || hit.T != 0.4 {
		t.Errorf("the webbed clip = %+v (%v), want the web's cell at t=0.4", hit, ok)
	}

	// The carpet the ray dips onto stops it through its own top face,
	// though the walk entered its cell through the west wall — the face
	// a placement reads is the shape's, not the walk's.
	carpet, ok := registry.FindState("minecraft:white_carpet", nil)
	if !ok {
		t.Fatal("the block registry has no white carpet")
	}
	if !w.SetBlock(8, 64, 4, carpet) {
		t.Fatal("the world did not take the carpet")
	}
	hit, ok = w.ClipBlocks(5.5, 64.8, 4.5, 4, -1, 0, nil)
	if !ok || hit.X != 8 || hit.Y != 64 || hit.Z != 4 || hit.Fluid || hit.Face != protocol.FaceTop {
		t.Errorf("the carpet clip = %+v (%v), want the carpet's cell through its top face", hit, ok)
	}
	if hit.T < 0.737 || hit.T > 0.738 {
		t.Errorf("the carpet clip's t = %f, want the carpet's top at 0.7375", hit.T)
	}
}

// TestClipBlocksPassesOverTheRails: every rail's outline is its 2/16 bed,
// 8/16 when it ascends, so a ray over it stops on the block behind.
func TestClipBlocksPassesOverTheRails(t *testing.T) {
	w := newRayWorld(t)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	if !w.SetBlock(10, 64, 0, stone) {
		t.Fatal("the world did not take the stone")
	}
	for _, name := range []string{"rail", "powered_rail", "detector_rail", "activator_rail"} {
		flat, ok := registry.FindState("minecraft:"+name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		if !w.SetBlock(9, 64, 0, flat) {
			t.Fatalf("the world did not take the %s", name)
		}
		hit, ok := w.ClipBlocks(8.5, 64.5, 0.5, 3, 0, 0, nil)
		if !ok || hit.X != 10 || hit.Y != 64 || hit.Z != 0 || hit.Face != protocol.FaceWest {
			t.Errorf("the clip over the flat %s = %+v (%v), want the stone's west face", name, hit, ok)
		}

		ascending, ok := registry.WithProp(flat, registry.PropShape, "ascending_east")
		if !ok {
			t.Fatalf("the %s has no ascending_east shape", name)
		}
		if !w.SetBlock(9, 64, 0, ascending) {
			t.Fatalf("the world did not take the ascending %s", name)
		}
		hit, ok = w.ClipBlocks(8.5, 64.4, 0.5, 3, 0, 0, nil)
		if !ok || hit.X != 9 || hit.Y != 64 || hit.Z != 0 || hit.Face != protocol.FaceWest {
			t.Errorf("the clip under the ascending %s's half = %+v (%v), want the rail's west face", name, hit, ok)
		}
		hit, ok = w.ClipBlocks(8.5, 64.6, 0.5, 3, 0, 0, nil)
		if !ok || hit.X != 10 || hit.Y != 64 || hit.Z != 0 || hit.Face != protocol.FaceWest {
			t.Errorf("the clip over the ascending %s's half = %+v (%v), want the stone's west face", name, hit, ok)
		}
	}
}

// TestOutlineBoxesReadsTheOutlines: the families the collision leaves
// empty carry the outlines vanilla draws them with, and the fluids carry
// none at all.
func TestOutlineBoxesReadsTheOutlines(t *testing.T) {
	t.Parallel()
	stateOf := func(name, props string) uint16 {
		t.Helper()
		state, ok := registry.FindState("minecraft:"+name, nil)
		if !ok {
			t.Fatalf("no state for minecraft:%s", name)
		}
		return state
	}

	if boxes := outlineBoxes(stateOf("water", "")); boxes != nil {
		t.Errorf("the water outline = %v, want none", boxes)
	}
	if boxes := outlineBoxes(stateOf("cobweb", "")); len(boxes) != 1 || boxes[0] != unitBox() {
		t.Errorf("the cobweb outline = %v, want the whole cube", boxes)
	}
	if boxes := outlineBoxes(stateOf("oak_slab", "")); len(boxes) != 1 ||
		boxes[0] != (box{0, 0, 0, 1, 0.5, 1}) {
		t.Errorf("the slab outline = %v, want the bottom half", boxes)
	}
	if boxes := outlineBoxes(stateOf("torch", "")); len(boxes) != 2 {
		t.Errorf("the torch outline = %v, want the stand and the tip", boxes)
	}
	if boxes := outlineBoxes(stateOf("redstone_wire", "")); len(boxes) != 1 ||
		boxes[0].y1 != 1.0/16 {
		t.Errorf("the wire outline = %v, want the sixteenth on the floor", boxes)
	}
	if boxes := outlineBoxes(stateOf("stone", "")); len(boxes) != 1 || boxes[0] != unitBox() {
		t.Errorf("the stone outline = %v, want the whole cube", boxes)
	}
}

// TestOutlineBoxesReadTheTallFamiliesOutlines checks the look ray's shape
// for the collision families whose outline is shorter than their collision:
// a fence's and a wall's 16-sixteenth posts and arms, a low wall side's 14,
// and a gate's plate held open or shut.
func TestOutlineBoxesReadTheTallFamiliesOutlines(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  []box
	}{
		{"minecraft:oak_fence", nil, []box{boxF(6, 0, 6, 10, 16, 10)}},
		{"minecraft:oak_fence", map[string]string{"north": "true"},
			[]box{boxF(6, 0, 6, 10, 16, 10), boxF(6, 0, 0, 10, 16, 6)}},
		{"minecraft:cobblestone_wall", nil, []box{boxF(4, 0, 4, 12, 16, 12)}},
		{"minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "low", "south": "tall", "up": "false", "west": "none"},
			[]box{boxF(5, 0, 0, 11, 14, 11), boxF(5, 0, 5, 11, 16, 16)}},
		{"minecraft:oak_fence_gate", nil, []box{boxF(0, 0, 6, 16, 16, 10)}},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true", "facing": "east"},
			[]box{boxF(6, 0, 0, 10, 16, 16)}},
		{"minecraft:oak_fence_gate", map[string]string{"in_wall": "true"},
			[]box{boxF(0, 0, 6, 16, 13, 10)}},
	} {
		got := outlineBoxes(mustFind(t, tc.name, tc.props))
		if len(got) != len(tc.want) {
			t.Errorf("the outline of %s %v = %v, want %v", tc.name, tc.props, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("outline box %d of %s %v = %v, want %v",
					i, tc.name, tc.props, got[i], tc.want[i])
			}
		}
	}
}

// strconvCell writes a cell the way the test reads it back.
func strconvCell(x, y, z int32) string {
	return fmt.Sprintf("%d,%d,%d", x, y, z)
}

// strconvFace writes a face number the way the test reads it back.
func strconvFace(face int32) string { return fmt.Sprintf("%d", face) }

// equalStrings answers whether the two lists name the same strings in the
// same order.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
