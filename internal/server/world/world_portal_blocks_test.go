package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestHasPortalBlocksTracksWritesAndChunkLoads(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}
	w.Acquire(pos)
	if w.HasPortalBlocks() {
		t.Fatal("an empty chunk reports portal blocks")
	}
	for _, b := range []registry.Block{registry.BlockNetherPortal, registry.BlockEndPortal, registry.BlockEndGateway} {
		w.SetBlock(1, 70, 1, b.DefaultState())
		if !w.HasPortalBlocks() {
			t.Errorf("%v written: HasPortalBlocks = false", b)
		}
		w.SetBlock(1, 70, 1, registry.BlockAir.DefaultState())
		if w.HasPortalBlocks() {
			t.Errorf("%v removed: HasPortalBlocks = true", b)
		}
	}
	w.SetBlock(1, 70, 1, registry.BlockEndGateway.DefaultState())
	var saved sync.WaitGroup
	w.ReleaseWait(pos, &saved)
	saved.Wait()
	if w.HasPortalBlocks() {
		t.Error("chunk unloaded: HasPortalBlocks = true")
	}
	w.Acquire(pos)
	if !w.HasPortalBlocks() {
		t.Error("chunk reloaded with a gateway: HasPortalBlocks = false")
	}
	w.Release(pos)
}
