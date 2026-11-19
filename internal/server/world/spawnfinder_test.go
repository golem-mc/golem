package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The spawn search's own tests: the square the respawn_radius rule draws
// holding every candidate the walk may land on, the suggestion itself taken
// when the radius leaves no room to move, the whole search falling back to
// the suggestion's height fixup when the square offers nothing to stand on,
// and the column scan reading the ground, the water and the hole the way
// the vanilla heightmaps read them.

// waterState is the fluid the flood tests pour, the source level the
// water block's default state carries.
var waterState, _ = registry.FindState("minecraft:water", nil)

// loadAround acquires the four chunks a square of blocks around the origin
// spans, so the flood the tests pour has somewhere to land: SetBlock works
// on loaded chunks only, the way a player's own edits do.
func loadAround(t *testing.T, w *World) {
	t.Helper()
	for x := int32(-1); x <= 0; x++ {
		for z := int32(-1); z <= 0; z++ {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
}

// TestAdjustedSpawnStaysInRadius checks that every candidate the walk may
// pick sits inside the square the respawn_radius rule draws around the
// suggestion, and that a radius of no room at all picks the suggestion
// itself — the block above the ground, where the flat world's spawn block
// stands.
func TestAdjustedSpawnStaysInRadius(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetIntRule(RuleRespawnRadius, 3)
	spawn := w.Spawn()
	clear := func(sp SpawnPos) bool { return true }

	for range 200 {
		got := w.AdjustedSpawn(spawn, clear, nil)
		if got.Y != GroundLevel {
			t.Fatalf("AdjustedSpawn y = %d, want the ground's own %d", got.Y, GroundLevel)
		}
		if got.X < spawn.X-3 || got.X > spawn.X+3 || got.Z < spawn.Z-3 || got.Z > spawn.Z+3 {
			t.Fatalf("AdjustedSpawn = (%d,%d,%d), outside the radius 3 square around (%d,%d)",
				got.X, got.Y, got.Z, spawn.X, spawn.Z)
		}
	}

	w.SetIntRule(RuleRespawnRadius, 0)
	for range 20 {
		if got := w.AdjustedSpawn(spawn, clear, nil); got != spawn {
			t.Fatalf("AdjustedSpawn with radius 0 = (%d,%d,%d), want the suggestion (%d,%d,%d)",
				got.X, got.Y, got.Z, spawn.X, spawn.Y, spawn.Z)
		}
	}
}

// TestAdjustedSpawnShrinksToBorder checks that a border of size 6 around the
// spawn keeps a respawn_radius 10 search within 3 blocks of the spawn.
func TestAdjustedSpawnShrinksToBorder(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetIntRule(RuleRespawnRadius, 10)
	spawn := w.Spawn()
	b := NewBorder()
	b.SetCenter(float64(spawn.X)+0.5, float64(spawn.Z)+0.5)
	b.SetSize(6)
	w.SetBorder(b)
	clear := func(sp SpawnPos) bool { return true }

	for range 200 {
		got := w.AdjustedSpawn(spawn, clear, nil)
		if got.X < spawn.X-3 || got.X > spawn.X+3 || got.Z < spawn.Z-3 || got.Z > spawn.Z+3 {
			t.Fatalf("AdjustedSpawn = (%d,%d,%d), outside 3 blocks of (%d,%d)",
				got.X, got.Y, got.Z, spawn.X, spawn.Z)
		}
	}
}

// TestRespawnColumnReadsTheGround checks the column scan's answer on plain
// ground: the block above the ground's top, with the scan's own refusal of
// the water column and its acceptance of the solid one beside it.
func TestRespawnColumnReadsTheGround(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())

	place, ok := w.RespawnColumn(8, 8)
	if !ok || place != (SpawnPos{X: 8, Y: GroundLevel, Z: 8}) {
		t.Fatalf("RespawnColumn on plain ground = (%d,%d,%d), %t; want (%d,%d,8), true",
			place.X, place.Y, place.Z, ok, 8, GroundLevel)
	}

	// A two-deep flood over one column leaves the scan nothing: the water
	// the walk from the top meets first is where it gives up.
	loadAround(t, w)
	w.SetBlock(9, GroundLevel-1, 9, waterState)
	w.SetBlock(9, GroundLevel, 9, waterState)
	if _, ok := w.RespawnColumn(9, 9); ok {
		t.Error("RespawnColumn of a flooded column = true, want false")
	}

	// A hole one deep leaves the scan the pit's own floor.
	w.SetBlock(10, GroundLevel-1, 10, airState)
	place, ok = w.RespawnColumn(10, 10)
	if !ok || place != (SpawnPos{X: 10, Y: GroundLevel - 1, Z: 10}) {
		t.Fatalf("RespawnColumn of a one-deep hole = (%d,%d,%d), %t; want the pit floor (%d,%d,10), true",
			place.X, place.Y, place.Z, ok, 10, GroundLevel-1)
	}
}

// TestAdjustedSpawnFallsBackWhenSquareIsWater checks the fallback: a square
// whose every column a body of water covers leaves the walk nothing, and
// the suggestion's own height fixup answers instead — carried up out of the
// water to the first clear air above it, the place the vanilla fixup's own
// walk comes to rest on.
func TestAdjustedSpawnFallsBackWhenSquareIsWater(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetIntRule(RuleRespawnRadius, 2)
	loadAround(t, w)
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			w.SetBlock(x, GroundLevel-1, z, waterState)
			w.SetBlock(x, GroundLevel, z, waterState)
			w.SetBlock(x, GroundLevel+1, z, waterState)
		}
	}

	got := w.AdjustedSpawn(w.Spawn(), func(SpawnPos) bool { return true }, nil)
	if got != (SpawnPos{X: 0, Y: GroundLevel + 2, Z: 0}) {
		t.Fatalf("AdjustedSpawn over a flooded square = (%d,%d,%d), want the fixup's rest above the water (0,%d,0)",
			got.X, got.Y, got.Z, GroundLevel+2)
	}
}

// TestFixupSpawnHeight checks the fixup's walk on its own: a suggestion
// buried under a block is carried to the first air above it, and one already
// standing clear is left exactly where it is.
func TestFixupSpawnHeight(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	loadAround(t, w)

	if got := w.FixupSpawnHeight(SpawnPos{X: 4, Y: GroundLevel, Z: 4}, nil); got != (SpawnPos{X: 4, Y: GroundLevel, Z: 4}) {
		t.Fatalf("FixupSpawnHeight of a clear suggestion = (%d,%d,%d), want it left alone", got.X, got.Y, got.Z)
	}

	w.SetBlock(5, GroundLevel, 5, stoneState)
	w.SetBlock(5, GroundLevel+1, 5, stoneState)
	if got := w.FixupSpawnHeight(SpawnPos{X: 5, Y: GroundLevel, Z: 5}, nil); got != (SpawnPos{X: 5, Y: GroundLevel + 2, Z: 5}) {
		t.Fatalf("FixupSpawnHeight of a buried suggestion = (%d,%d,%d), want (%d,%d,5)",
			got.X, got.Y, got.Z, 5, GroundLevel+2)
	}
}
