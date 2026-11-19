package entity

import (
	"testing"
)

// The squid's own halves, driven against the stub world: the flee's scaling,
// the despawn the categories share, the water cells the spawn rules accept,
// and the ink and dark ticks a hurt leaves behind.

// TestSquidSurfaceSpawnTakesAFluidCell checks the surface rule's water: the
// IN_WATER placement reads the cell's fluid, so a kelp cell is taken where the
// water block alone would not be, and the block above must be the water block
// itself, so a seagrass above is refused.
// vanilla: AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules,
// SpawnPlacementTypes.IN_WATER
func TestSquidSurfaceSpawnTakesAFluidCell(t *testing.T) {
	w := newSpawnTestWorld()
	x, z := atChunk(0, 0)
	const y = 60
	water := defaultState(t, "minecraft:water")
	kelp := defaultState(t, "minecraft:kelp")
	seagrass := defaultState(t, "minecraft:seagrass")
	w.set(x, y-1, z, water)
	w.set(x, y, z, water)
	w.set(x, y+1, z, water)
	if !squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Fatal("a column of water was refused a surface squid")
	}
	w.set(x, y, z, kelp)
	if !squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Error("a kelp cell was refused the surface squid's water")
	}
	w.set(x, y, z, water)
	w.set(x, y+1, z, seagrass)
	if squidSpawnPositionOk(w, CategoryWaterCreature, x, y, z) {
		t.Error("a seagrass above was taken as the surface squid's water")
	}
}
