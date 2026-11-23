package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestLoadedTouchBlocksTracksWritesAndChunkLifetime(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{}
	plate := registry.BlockLightWeightedPressurePlate.DefaultState()
	powered, ok := registry.WithInt(plate, registry.PropPower, 3)
	if !ok || powered == plate {
		t.Fatal("could not set pressure plate power")
	}
	check := func(step string, want int) {
		t.Helper()
		if got := w.LoadedTouchBlocks(); got != want {
			t.Fatalf("%s: loaded touch blocks = %d, want %d", step, got, want)
		}
	}

	w.SetBlock(0, GroundLevel, 0, plate)
	check("write to an unloaded chunk", 0)
	w.Acquire(pos)
	check("load flat chunk", 0)
	w.SetBlock(0, worldMinY-1, 0, plate)
	w.SetBlocks([]BlockChange{{Y: worldMinY + worldHeight, State: plate}})
	check("writes outside build height", 0)

	w.SetBlock(0, GroundLevel, 0, plate)
	check("place first plate", 1)
	w.SetBlock(0, GroundLevel, 0, powered)
	check("change only plate power", 1)
	w.SetBlock(0, GroundLevel, 0, powered)
	check("write the same state again", 1)

	rail := registry.BlockDetectorRail.DefaultState()
	w.SetBlocks([]BlockChange{
		{Y: GroundLevel, State: rail},
		{Y: GroundLevel, State: powered},
		{X: 1, Y: GroundLevel, State: plate},
	})
	check("replace a touch block twice and add another", 2)
	w.SetBlocks([]BlockChange{
		{Y: GroundLevel, State: airState},
		{X: 1, Y: GroundLevel, State: stoneState},
	})
	check("replace the last plates in a batch", 0)

	w.SetBlock(0, GroundLevel, 0, plate)
	w.Acquire(pos)
	check("acquire an already loaded chunk", 1)
	w.Release(pos)
	check("release one of two holds", 1)
	w.Release(pos)
	w.FlushSaves()
	check("unload after saving", 0)

	w.Acquire(pos)
	check("load the saved plate", 1)
	w.Release(pos)
	check("unload a clean chunk", 0)
}
