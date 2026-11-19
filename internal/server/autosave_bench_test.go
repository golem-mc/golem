package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// autoSaveBenchPlayers is how many players with statistics and advancements
// BenchmarkAutoSaveSnapshotPlayers adds beside the tracker server's eight.
const autoSaveBenchPlayers = 100

// BenchmarkAutoSaveSnapshotPlayers times the tick's share of an auto-save
// with 108 players online; the files written beside the tick are untimed.
func BenchmarkAutoSaveSnapshotPlayers(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	s.mu.Lock()
	for i := range autoSaveBenchPlayers {
		p := &player.Player{UUID: uuid.New(), Name: fmt.Sprintf("p%d", i), EntityID: s.nextEntity()}
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		c.stats.Store(player.NewStats())
		c.advancements.Store(advancement.NewProgress(advancement.Current()))
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	s.mu.Unlock()
	b.ResetTimer()
	for b.Loop() {
		s.savingMu.Lock()
		flush := s.autoSaveSnapshot()
		b.StopTimer()
		flush()
		s.savingMu.Unlock()
		b.StartTimer()
	}
}
