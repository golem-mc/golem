package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestNeighbourStateProperties checks state changes keep properties the
// neighbour did not change, including fluid and attachment state.
func TestNeighbourStateProperties(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		return registry.MustState("minecraft:"+name, props)
	}
	const x, y, z = 5, GroundLevel + 2, 5
	check := func(want uint16) {
		t.Helper()
		if got := w.BlockAt(x, y, z); got != want {
			t.Fatalf("state = %d, want %d", got, want)
		}
	}
	var changes []BlockChange
	mushroom := state("brown_mushroom_block", map[string]string{"north": "false", "up": "false"})
	w.SetBlock(x, y, z, mushroom)
	p.updateAsNeighbor(x, y, z, &changes, false, airState, mushroom, [3]int32{1, 0, 0})
	check(state("brown_mushroom_block", map[string]string{"north": "false", "up": "false", "east": "false"}))

	w.SetBlock(x, y, z, state("grass_block", nil))
	p.updateAsNeighbor(x, y, z, &changes, false, airState, state("snow", nil), [3]int32{0, 1, 0})
	check(state("grass_block", map[string]string{"snowy": "true"}))

	// A wet dripleaf becomes a wet stem facing the same way when a leaf
	// appears above; the old leaf's tilt does not belong to the stem.
	w.SetBlock(x, y-1, z, registry.BlockClay.DefaultState())
	w.SetBlock(x, y, z, state("big_dripleaf", map[string]string{"facing": "west", "waterlogged": "true", "tilt": "full"}))
	w.SetBlock(x, y+1, z, state("big_dripleaf", nil))
	p.updateAsNeighbor(x, y, z, &changes, false, airState, w.BlockAt(x, y+1, z), [3]int32{0, 1, 0})
	check(state("big_dripleaf_stem", map[string]string{"facing": "west", "waterlogged": "true"}))

	// Losing one of two attached faces keeps the other face and the water.
	w.SetBlock(x, y+1, z, airState)
	w.SetBlock(x, y, z, state("glow_lichen", map[string]string{"north": "true", "east": "true", "waterlogged": "true"}))
	w.SetBlock(x, y, z-1, stoneState)
	w.SetBlock(x+1, y, z, airState)
	p.trimFaces(x, y, z, w.BlockAt(x, y, z), dirEast, &changes)
	check(state("glow_lichen", map[string]string{"north": "true", "waterlogged": "true"}))

	w.SetBlock(x, y, z, state("bell", map[string]string{"attachment": "double_wall", "facing": "east", "powered": "true"}))
	p.bellShapeUpdate(x, y, z, [3]int32{-1, 0, 0}, w.BlockAt(x, y, z), &changes)
	check(state("bell", map[string]string{"attachment": "single_wall", "facing": "west", "powered": "true"}))
}

func TestLeafAndScaffoldingStateProperties(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	const x, y, z = 5, GroundLevel + 2, 5
	w.SetBlock(x+1, y, z, registry.BlockOakLog.DefaultState())
	leaf := registry.MustState("minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "true", "waterlogged": "true"})
	w.SetBlock(x, y, z, leaf)
	var changes []BlockChange
	p.leafTick(x, y, z, leaf, &changes)
	want := registry.MustState("minecraft:oak_leaves", map[string]string{"distance": "1", "persistent": "true", "waterlogged": "true"})
	if got := w.BlockAt(x, y, z); got != want {
		t.Fatalf("leaf state = %d, want %d", got, want)
	}

	w.SetBlock(x, y-1, z, stoneState)
	scaffold := registry.MustState("minecraft:scaffolding", map[string]string{"distance": "7", "bottom": "true", "waterlogged": "true"})
	got := w.ScaffoldingStateForPlacement(scaffold, x, y, z)
	want = registry.MustState("minecraft:scaffolding", map[string]string{"distance": "0", "bottom": "false", "waterlogged": "true"})
	if got != want {
		t.Fatalf("scaffolding state = %d, want %d", got, want)
	}
}

// TestMossCarpetSides checks base and topper sides where their supports
// disagree, and that an existing side is kept when no new sides are requested.
func TestMossCarpetSides(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	const x, y, z = 5, GroundLevel + 2, 5
	carpet := registry.BlockPaleMossCarpet.DefaultState()
	w.SetBlock(x, y, z-1, stoneState)
	w.SetBlock(x+1, y, z, stoneState)
	above := registry.MustState(PaleMossCarpet, map[string]string{"bottom": "false", "north": "low"})
	w.SetBlock(x, y+1, z, above)
	got := MossyCarpetState(w, carpet, x, y, z, false)
	want := registry.MustState(PaleMossCarpet, map[string]string{"north": "tall", "east": "low"})
	if got != want {
		t.Fatalf("base = %d, want %d", got, want)
	}
	w.SetBlock(x, y, z, got)
	w.SetBlock(x, y+1, z, airState)
	w.SetBlock(x, y+1, z-1, stoneState)
	w.SetBlock(x+1, y+1, z, stoneState)
	calls := 0
	topper, ok := MossyCarpetTopper(w, x, y, z, func() bool { calls++; return true })
	want = registry.MustState(PaleMossCarpet, map[string]string{"bottom": "false", "north": "low", "east": "low"})
	if !ok || topper != want || calls != 2 {
		t.Fatalf("topper = %d, %v, %d rolls; want %d, true, 2", topper, ok, calls, want)
	}
	w.SetBlock(x, y, z, registry.MustState(PaleMossCarpet, map[string]string{"east": "low"}))
	got = MossyCarpetState(w, topper, x, y+1, z, false)
	want = registry.MustState(PaleMossCarpet, map[string]string{"bottom": "false", "east": "low"})
	if got != want {
		t.Fatalf("trimmed topper = %d, want %d", got, want)
	}
	w.SetBlock(x, y+1, z, ^uint16(0))
	if _, ok := MossyCarpetTopper(w, x, y, z, func() bool {
		t.Fatal("invalid state rolled for topper sides")
		return true
	}); ok {
		t.Fatal("topper replaced an invalid state")
	}
}

// TestDryCoralTwinProperties covers every living coral family, including all
// wall-fan facings, through the scheduled tick's replacement path.
func TestDryCoralTwinProperties(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	for _, species := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		for _, form := range []string{"coral_block", "coral", "coral_fan", "coral_wall_fan"} {
			for _, facing := range []string{"north", "east", "south", "west"} {
				props := map[string]string{}
				if form != "coral_block" {
					props["waterlogged"] = "false"
				}
				if form == "coral_wall_fan" {
					props["facing"] = facing
				}
				name := species + "_" + form
				state := registry.MustState("minecraft:"+name, props)
				w.SetBlock(5, GroundLevel+2, 5, state)
				var changes []BlockChange
				p.coralTick(5, GroundLevel+2, 5, state, &changes)
				want := registry.MustState("minecraft:dead_"+name, props)
				if got := w.BlockAt(5, GroundLevel+2, 5); got != want {
					t.Fatalf("%s %v died as %d, want %d", name, props, got, want)
				}
				if form != "coral_wall_fan" {
					break
				}
			}
		}
	}
}
