package server

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// TestSpawningChunkListFollowsLoadedSet checks the cached sorted chunk list is
// rebuilt after a chunk loads and another unloads.
func TestSpawningChunkListFollowsLoadedSet(t *testing.T) {
	s, w := newBlockBenchServer(t, 3)
	addBlockBenchPlayers(t, s, w, 1, 0, 0)
	dim := s.dimensionOfWorld(w)
	fresh := func() []world.ChunkPos {
		var want []world.ChunkPos
		s.mu.RLock()
		clients := s.clientsLocked()
		s.mu.RUnlock()
		players := spawnPlayersOf(clients, w)
		for _, pos := range w.AppendLoadedChunkPositions(nil) {
			if chunkIsSpawning(players, pos) {
				want = append(want, pos)
			}
		}
		world.SortChunkPositions(want)
		return want
	}
	s.tickNaturalSpawning()
	if got := slices.Clone(dim.spawningPositions); len(got) == 0 || !slices.Equal(got, fresh()) {
		t.Fatalf("first tick: got %v, want %v", got, fresh())
	}
	added, removed := world.ChunkPos{X: 5, Z: 5}, world.ChunkPos{X: 3, Z: 3}
	w.Acquire(added)
	var saved sync.WaitGroup
	w.ReleaseWait(removed, &saved)
	saved.Wait()
	s.tickNaturalSpawning()
	want := fresh()
	if !slices.Equal(dim.spawningPositions, want) || !slices.Contains(want, added) || slices.Contains(want, removed) {
		t.Fatalf("after load/unload: got %v, want %v", dim.spawningPositions, want)
	}
}
