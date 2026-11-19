package storage

import (
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestSetLoadedBlockWaitsOnlyForItsOwnSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)
	servertest.HoldTicks(t, srv)

	chunkEntered, chunkRelease := make(chan struct{}), make(chan struct{})
	entityEntered, entityRelease := make(chan struct{}), make(chan struct{})
	var chunkOnce, entityOnce sync.Once
	unparkChunk := func() { chunkOnce.Do(func() { close(chunkRelease) }) }
	unparkEntity := func() { entityOnce.Do(func() { close(entityRelease) }) }
	done := make(chan struct{})
	t.Cleanup(func() {
		unparkChunk()
		unparkEntity()
		<-done
	})
	srv.SetChunkWriteGate(func() { close(chunkEntered); <-chunkRelease })
	srv.SetEntityWriteGate(func() { close(entityEntered); <-entityRelease })
	const x, y, z = int32(517), int32(64), int32(5)
	stone := registry.BlockStone.DefaultState()
	go func() {
		srv.SetLoadedBlock(x, y, z, stone)
		close(done)
	}()
	select {
	case <-chunkEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("block save never reached disk")
	}
	select {
	case <-done:
		t.Fatal("SetLoadedBlock returned before its chunk save finished")
	default:
	}

	// Queue an unload beside the helper's wait. Let the chunk write finish
	// first so this also works with a single I/O runner.
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 96}
	w.QueueChunkEntitiesStamped(pos, w.StampChunkEntities(pos), nil)
	unparkChunk()
	select {
	case <-entityEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("entity save never reached disk")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SetLoadedBlock waited for an unrelated entity save")
	}
	if !blockIsSaved(t, cfg, x, y, z, stone) {
		t.Fatal("SetLoadedBlock returned without saving its block to disk")
	}
}
