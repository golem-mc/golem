package server

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestRandomTickChunksCacheManyPlayers checks the cache survives map order
// with several players and drops on a chunk crossing, a ticket and a reach
// change.
func TestRandomTickChunksCacheManyPlayers(t *testing.T) {
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
	var players []*player.Player
	for i := range 8 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: float64(i)*160 + 8, Y: float64(world.GroundLevel), Z: 8})
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		s.mu.Lock()
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		s.mu.Unlock()
		players = append(players, p)
	}
	defer func() {
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
	}()
	// hit reports whether the next refresh kept the list (a sentinel
	// survives) instead of regathering it.
	sentinel := world.ChunkPos{X: 1 << 20}
	hit := func() bool {
		dim.randomTickPositions = append(dim.randomTickPositions, sentinel)
		s.refreshRandomTickChunks(dim)
		return slices.Contains(dim.randomTickPositions, sentinel)
	}
	s.refreshRandomTickChunks(dim)
	for i := range 50 {
		if !hit() {
			t.Fatalf("refresh %d regathered with nothing changed", i)
		}
	}
	players[3].SetPosition(player.Position{X: 3*160 + 8 + 64, Y: float64(world.GroundLevel), Z: 8})
	if hit() {
		t.Fatal("a player crossing a chunk border kept the list")
	}
	if !hit() {
		t.Fatal("refresh after the crossing regathered again")
	}
	tp := world.ChunkPos{X: 500, Z: 500}
	dim.w.AddTicket(tp, 1)
	if hit() {
		t.Fatal("an added ticket kept the list")
	}
	dim.w.RemoveTicket(tp, 1)
	if hit() {
		t.Fatal("a removed ticket kept the list")
	}
	s.simulationDistance.Add(1)
	if hit() {
		t.Fatal("a simulation distance change kept the list")
	}
	if !hit() {
		t.Fatal("refresh after the changes regathered again")
	}
}
