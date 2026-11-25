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
	p.trimFaces(x, y, z, w.BlockAt(x, y, z), dirEast, &changes, false)
	check(state("glow_lichen", map[string]string{"north": "true", "waterlogged": "true"}))

	w.SetBlock(x, y, z, state("bell", map[string]string{"attachment": "double_wall", "facing": "east", "powered": "true"}))
	p.bellShapeUpdate(x, y, z, [3]int32{-1, 0, 0}, w.BlockAt(x, y, z), &changes)
	check(state("bell", map[string]string{"attachment": "single_wall", "facing": "west", "powered": "true"}))
}
