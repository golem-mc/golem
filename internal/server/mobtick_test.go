package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestMobTickInputsForgetDepartedEntitiesAndChunks(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.allDims()[0].w
	pos := world.ChunkPos{}
	w.Acquire(pos)
	w.FlushLighting()
	s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, 0.5, 64, 0.5, 0, 0, 0, 32767)
	item := s.items[0]
	in := s.resetMobTickInputs()
	known := s.readMobTickInputs(in)
	if !known[item.uuid] {
		t.Fatal("live item missing from refusal set")
	}
	if _, ok := in.loadedBefore[w][pos]; !ok {
		t.Fatal("loaded chunk missing from snapshot")
	}
	// Non-tick callers own their set even when the next tick reuses its maps.
	independent := s.liveEntityUUIDs()
	item.item.Dead = true
	var saves sync.WaitGroup
	w.ReleaseWait(pos, &saves)
	saves.Wait()
	if w.ChunkLoaded(pos) {
		t.Fatal("chunk did not unload")
	}
	in = s.resetMobTickInputs()
	known = s.readMobTickInputs(in)
	if known[item.uuid] {
		t.Fatal("departed item still refuses a saved copy")
	}
	if _, ok := in.loadedBefore[w][pos]; ok {
		t.Fatal("unloaded chunk still appears loaded")
	}
	if !independent[item.uuid] {
		t.Fatal("tick changed another caller's refusal set")
	}
}
