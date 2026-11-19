package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestNearChunkDisplacesQueuedGeneration(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	w.SetDropUntouchedChunks(true)
	gate := make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() { unpark(); w.Close() })
	w.SetChunkGenerationGate(func(world.ChunkPos) { <-gate })
	c := &client{
		loadedWorld: w,
		loaded:      newChunkSet(world.ChunkPos{}, 200),
		generating:  make(map[world.ChunkPos]*world.ChunkRequest),
		radius:      200, load: 200,
	}
	// All remain inside the load radius, but a newly approached centre must
	// enter the bounded window before any of these finish generating.
	for i := range maxGenerating {
		pos := world.ChunkPos{X: int32(100 + i)}
		c.loaded.add(pos)
		c.generating[pos] = w.RequestChunk(pos, world.DistanceTo(pos, c.center))
	}
	near, far := world.ChunkPos{}, world.ChunkPos{X: 100 + maxGenerating - 1}
	c.pendingGeneration = []world.ChunkPos{near}
	c.pruneGenerationWindow()
	c.submitGeneration()
	if c.generating[near] == nil {
		t.Error("nearby chunk did not enter the generation window")
	}
	if c.generating[far] != nil {
		t.Error("farthest queued chunk kept its generation slot")
	}
	if len(c.generating) != maxGenerating {
		t.Errorf("window = %d, want %d", len(c.generating), maxGenerating)
	}
	c.cancelGenerating(nil)
}
