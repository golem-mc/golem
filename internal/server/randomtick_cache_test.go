package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestRandomTickChunksCache checks the cached ticking list follows a forced
// chunk and a loaded chunk, and is sorted.
func TestRandomTickChunksCache(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpawning(false)
	defer s.Shutdown()
	dim := s.allDims()[0]
	s.refreshRandomTickChunks(dim)
	if !slices.Equal(dim.randomTickPositions, s.randomTickSelectionSorted(dim)) {
		t.Fatal("initial list differs from a fresh gather")
	}
	pos := world.ChunkPos{X: 40, Z: -7}
	dim.w.SetChunkForced(pos, true)
	s.refreshRandomTickChunks(dim)
	if !slices.Contains(dim.randomTickPositions, pos) {
		t.Fatal("forced chunk missing from the cached list")
	}
	if !slices.Equal(dim.randomTickPositions, s.randomTickSelectionSorted(dim)) {
		t.Fatal("list after forcing differs from a fresh gather")
	}
	dim.w.SetChunkForced(pos, false)
	s.refreshRandomTickChunks(dim)
	if slices.Contains(dim.randomTickPositions, pos) {
		t.Fatal("unforced chunk still in the cached list")
	}
}

func (s *Server) randomTickSelectionSorted(dim *dimension) []world.ChunkPos {
	cs := s.randomTickSelection(dim)
	world.SortChunkPositions(cs)
	return cs
}
