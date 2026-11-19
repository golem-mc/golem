package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// manyStates returns n distinct block states of one block, in the registry's
// own order.
func manyStates(t *testing.T, block string, n int) []uint16 {
	t.Helper()
	entries := registry.AllStates[block]
	if len(entries) < n {
		t.Fatalf("%s has %d states, want at least %d", block, len(entries), n)
	}
	states := make([]uint16, n)
	for i := range states {
		states[i] = entries[i].ID
	}
	return states
}

// TestASectionOfManyDistinctStatesSurvivesASave fills one chunk section with
// 1100 distinct block states and reads them back from the reopened region
// file. The palette needs 11 bits, which pack into 820 longs; inferring the
// width from the long count instead reads 12-bit entries and scrambles the
// section. vanilla: PalettedContainer.unpack
func TestASectionOfManyDistinctStatesSurvivesASave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	// Stop the loop before writing: redstone wire would otherwise be updated
	// by a physics tick between the writes and the save.
	srv.StopTick()

	states := manyStates(t, "minecraft:redstone_wire", 1100)
	release := srv.HoldChunk(0, 0)
	for i, state := range states {
		srv.SetBlock(int32(i%16), int32(64+i/256), int32(i/16%16), state)
	}
	release()
	srv.FlushSaves()
	srv.Shutdown()

	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)

	wrong := 0
	for i, state := range states {
		x, y, z := int32(i%16), int32(64+i/256), int32(i/16%16)
		if got := w.BlockAt(x, y, z); got != state {
			wrong++
		}
	}
	if wrong != 0 {
		t.Errorf("%d of %d blocks read back wrong", wrong, len(states))
	}
}
