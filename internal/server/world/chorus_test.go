package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The chorus tests drive the random tick and the shape pass by hand, so the
// dice and the neighbour state are the test's to place. A flower on an end
// stone pillar grows a stem upward, joins the flower above through the shape
// pass, branches while young, and dies when it can do neither.

// chorusState resolves a chorus block state, failing the test when the
// registry does not have it.
func chorusState(t *testing.T, name string, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, overrides)
	if !ok {
		t.Fatalf("the registry has no %s with %v", name, overrides)
	}
	return state
}

// newChorusWorld is a flat world with the dice pinned, ready for a chorus
// flower's random tick to be driven directly. It builds the world itself
// rather than through newPhysicsWorld, which marks a test parallel once only.
func newChorusWorld(t *testing.T, seed int64) (*World, *Physics) {
	t.Helper()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(seed))
	return w, p
}

// put is a raw write that skips the neighbour update, so a test can lay out a
// state the shape pass is about to act on.
func put(t *testing.T, w *World, x, y, z int32, state uint16) {
	t.Helper()
	if !w.SetBlock(x, y, z, state) {
		t.Fatalf("setup: SetBlock(%d, %d, %d, %d) did not take", x, y, z, state)
	}
}

// TestChorusGrownFlowerWriteFailurePlaysNoEvent checks that a growth whose
// write did not take — an unloaded cell reads as air — leaves no level event
// behind.
func TestChorusGrownFlowerWriteFailurePlaysNoEvent(t *testing.T) {
	_, p := newChorusWorld(t, 0)
	var changes []BlockChange
	p.placeGrownFlower(1000, 64, 1000, 0, &changes)
	if len(p.randomEffects) != 0 {
		t.Errorf("a growth write that did not take played %v", p.randomEffects)
	}
}

// TestChorusGrownStemJoinsTheFlowerAbove checks the blocking fix: the stem
// grown under a new flower connects up to it through the write's shape pass,
// not only down to its support.
func TestChorusGrownStemJoinsTheFlowerAbove(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	put(t, w, 0, 63, 0, chorusState(t, "minecraft:end_stone", nil))
	put(t, w, 0, 64, 0, chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true"}))

	flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
	put(t, w, 0, 65, 0, flower)
	var changes []BlockChange
	p.chorusFlowerTick(0, 65, 0, flower, &changes)

	want := chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "true"})
	if got := w.BlockAt(0, 65, 0); got != want {
		t.Errorf("the stem at (0, 65, 0) = %d, want %d (down and up set)", got, want)
	}
}

// TestChorusBranchBaseJoinsTheBranch checks a sideways growth: the base
// becomes a plant whose property toward the branch flower is set.
func TestChorusBranchBaseJoinsTheBranch(t *testing.T) {
	w, p := newChorusWorld(t, 3)
	put(t, w, 0, 64, 0, chorusState(t, "minecraft:end_stone", nil))
	put(t, w, 0, 65, 0, chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "true"}))
	// The stone beside the cell above the flower blocks the upward growth, so
	// the tick can only branch.
	put(t, w, 1, 67, 0, chorusState(t, "minecraft:stone", nil))

	flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
	put(t, w, 0, 66, 0, flower)
	var changes []BlockChange
	p.chorusFlowerTick(0, 66, 0, flower, &changes)

	baseName, baseProps, _ := registry.BlockStateName(w.BlockAt(0, 66, 0))
	if baseName != "minecraft:chorus_plant" {
		t.Fatalf("the branch's base = %q, want a chorus plant", baseName)
	}
	props := registry.ParseStateProps(baseProps)
	branched := false
	for _, d := range chorusHorizontalDirs {
		delta := dirDelta[d]
		if registryStateName(w.BlockAt(delta[0], 66, delta[2])) == "minecraft:chorus_flower" {
			if props[faceProps[d]] != "true" {
				t.Errorf("the base's %s connection = %q, want true for the branch", faceProps[d], props[faceProps[d]])
			}
			branched = true
		}
	}
	if !branched {
		t.Error("the tick made no branch flower at the base's side")
	}
}

// TestChorusBranchSupportPillarAddsOne checks the support-pillar +1: a flower
// sitting on a plant that stands on a support block always draws at least one
// branch attempt, where the same dice without the support below can draw
// none.
func TestChorusBranchSupportPillarAddsOne(t *testing.T) {
	onSupport := func(seed int64) int {
		w, p := newChorusWorld(t, seed)
		put(t, w, 0, 64, 0, chorusState(t, "minecraft:end_stone", nil))
		put(t, w, 0, 65, 0, chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "true"}))
		put(t, w, 1, 67, 0, chorusState(t, "minecraft:stone", nil))
		flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
		put(t, w, 0, 66, 0, flower)
		var changes []BlockChange
		p.chorusFlowerTick(0, 66, 0, flower, &changes)
		return chorusBranchesAround(w, 0, 66, 0)
	}
	withoutSupport := func(seed int64) int {
		w, p := newChorusWorld(t, seed)
		put(t, w, 0, 65, 0, chorusState(t, "minecraft:end_stone", nil))
		put(t, w, 1, 67, 0, chorusState(t, "minecraft:stone", nil))
		flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
		put(t, w, 0, 66, 0, flower)
		var changes []BlockChange
		p.chorusFlowerTick(0, 66, 0, flower, &changes)
		return chorusBranchesAround(w, 0, 66, 0)
	}
	sawZero := false
	for seed := range int64(32) {
		if n := onSupport(seed); n == 0 {
			t.Errorf("seed %d: the support-pillar flower made no branch", seed)
		}
		if n := withoutSupport(seed); n == 0 {
			sawZero = true
		}
	}
	if !sawZero {
		t.Error("no seed without a support below made zero branches")
	}
}

// chorusBranchesAround counts the chorus flowers in the four horizontal cells
// around a base.
func chorusBranchesAround(w *World, x, y, z int32) int {
	n := 0
	for _, d := range chorusHorizontalDirs {
		delta := dirDelta[d]
		name, _, ok := registry.BlockStateName(w.BlockAt(x+delta[0], y, z+delta[2]))
		if ok && name == "minecraft:chorus_flower" {
			n++
		}
	}
	return n
}

// TestChorusAgeFourFlowerDiesWithoutBranching checks the age guard: at age
// four a flower that cannot grow up does not branch at all, it dies.
func TestChorusAgeFourFlowerDiesWithoutBranching(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	put(t, w, 0, 63, 0, chorusState(t, "minecraft:end_stone", nil))
	// The stone beside the cell above blocks the upward growth.
	put(t, w, 1, 65, 0, chorusState(t, "minecraft:stone", nil))

	flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "4"})
	put(t, w, 0, 64, 0, flower)
	var changes []BlockChange
	p.chorusFlowerTick(0, 64, 0, flower, &changes)

	dead := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "5"})
	if got := w.BlockAt(0, 64, 0); got != dead {
		t.Errorf("the age-four flower = %d, want %d (age five)", got, dead)
	}
	if n := chorusBranchesAround(w, 0, 64, 0); n != 0 {
		t.Errorf("the age-four flower made %d branches, want none", n)
	}
	if len(p.randomEffects) != 1 || p.randomEffects[0].LevelEvent != 1034 {
		t.Errorf("the age-four death played %v, want level event 1034", p.randomEffects)
	}
}

// TestChorusFlowerAtTopOfWorldStops checks the build-height return: a flower
// with no room left above it in the world does nothing at all.
func TestChorusFlowerAtTopOfWorldStops(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	top := w.MinY() + w.Height() - 1
	put(t, w, 0, top-1, 0, chorusState(t, "minecraft:end_stone", nil))
	flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
	put(t, w, 0, top, 0, flower)

	var changes []BlockChange
	p.chorusFlowerTick(0, top, 0, flower, &changes)

	if got := w.BlockAt(0, top, 0); got != flower {
		t.Errorf("the top-of-world flower = %d, want %d (unchanged)", got, flower)
	}
	if len(changes) != 0 || len(p.randomEffects) != 0 {
		t.Errorf("the top-of-world flower changed %v with events %v, want nothing", changes, p.randomEffects)
	}
}

// TestChorusFlowerUnderABlockStops checks the other early return: a flower
// with a solid block directly above it does nothing.
func TestChorusFlowerUnderABlockStops(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	put(t, w, 0, 63, 0, chorusState(t, "minecraft:end_stone", nil))
	flower := chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
	put(t, w, 0, 64, 0, flower)
	put(t, w, 0, 65, 0, chorusState(t, "minecraft:stone", nil))

	var changes []BlockChange
	p.chorusFlowerTick(0, 64, 0, flower, &changes)

	if got := w.BlockAt(0, 64, 0); got != flower {
		t.Errorf("the covered flower = %d, want %d (unchanged)", got, flower)
	}
	if len(changes) != 0 || len(p.randomEffects) != 0 {
		t.Errorf("the covered flower changed %v with events %v, want nothing", changes, p.randomEffects)
	}
}

// TestChorusPlantShapeUpdateSetsConnection checks the shape pass: a plant
// whose neighbour changed sets the connection property of that direction,
// here up for the flower above.
func TestChorusPlantShapeUpdateSetsConnection(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	put(t, w, 0, 63, 0, chorusState(t, "minecraft:end_stone", nil))
	plant := chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "false"})
	put(t, w, 0, 64, 0, plant)
	put(t, w, 0, 65, 0, chorusState(t, "minecraft:chorus_flower", map[string]string{"age": "0"}))

	var changes []BlockChange
	// d points from the changed flower down to the plant it sits on.
	p.shapeUpdate(0, 64, 0, [3]int32{0, -1, 0}, &changes)

	want := chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "true"})
	if got := w.BlockAt(0, 64, 0); got != want {
		t.Errorf("the plant = %d, want %d (up set by the flower above)", got, want)
	}
}

// TestChorusPlantShapeUpdateBreaksWithoutSupport checks the other half of the
// shape pass: a plant that lost its hold books its one-tick break.
func TestChorusPlantShapeUpdateBreaksWithoutSupport(t *testing.T) {
	w, p := newChorusWorld(t, 0)
	put(t, w, 0, 63, 0, chorusState(t, "minecraft:end_stone", nil))
	plant := chorusState(t, "minecraft:chorus_plant", map[string]string{"down": "true"})
	put(t, w, 0, 64, 0, plant)
	// Take the support away behind the engine's back.
	put(t, w, 0, 63, 0, airState)

	var changes []BlockChange
	// d points from the changed support up to the plant.
	p.shapeUpdate(0, 64, 0, [3]int32{0, 1, 0}, &changes)

	if !p.pending[blockKey{0, 64, 0}] {
		t.Error("the unsupported plant booked no break")
	}
}
