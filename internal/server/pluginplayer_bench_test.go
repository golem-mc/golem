package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/google/uuid"
)

// BenchmarkPlayerDisplayName covers the chat and command component boundary without plugins.
func BenchmarkPlayerDisplayName(b *testing.B) {
	c := &client{srv: &Server{scoreboard: scoreboard.New(nil)}, player: &player.Player{UUID: uuid.New(), Name: "Player"}}
	b.ReportAllocs()
	for b.Loop() {
		if c.DisplayName().Plain() != "Player" {
			b.Fatal("display name")
		}
	}
}

// BenchmarkPlayerSnapshot covers autosave serialization without timing file writes.
func BenchmarkPlayerSnapshot(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	c := s.playerClients()[0]
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := c.snapshotPlayerData(nil, nil); !ok {
			b.Fatal("missing player snapshot")
		}
	}
}
