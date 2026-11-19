package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkScoreboardBroadcast covers the main board's packet fanout without plugins.
func BenchmarkScoreboardBroadcast(b *testing.B) {
	s := &Server{clients: make(map[uuid.UUID]*client)}
	s.scoreboard = s.newScoreboard(b.TempDir())
	s.deferWakes.Store(true)
	for range 8 {
		id := uuid.New()
		c := &client{srv: s, player: &player.Player{UUID: id}}
		c.out = &mailbox{c: c}
		s.clients[id] = c
	}
	criteria, _ := scoreboard.LookupCriteria("dummy")
	s.scoreboard.AddObjective("bench", criteria, nbt.String("Bench"))
	s.scoreboard.SetDisplay(1, "bench")
	b.ReportAllocs()
	for b.Loop() {
		s.scoreboard.Update([]scoreboard.Holder{{Name: "entry"}}, "bench", func(v int32) int32 { return v + 1 })
		for _, c := range s.clients {
			clear(c.out.queue)
			c.out.queue = c.out.queue[:0]
			c.out.queued = 0
		}
	}
}

// BenchmarkScoreCriteria measures an unchanged player's gameplay criteria without plugins.
func BenchmarkScoreCriteria(b *testing.B) {
	s := &Server{scoreboard: scoreboard.New(nil)}
	c := &client{srv: s, player: &player.Player{UUID: uuid.New(), Name: "Bench"}}
	c.stats.Store(player.NewStats())
	c.tickScoreCriteria()
	b.ReportAllocs()
	for b.Loop() {
		c.tickScoreCriteria()
		c.awardStat(player.Custom(registry.CustomStatPlayTime), 1)
	}
}

// BenchmarkBossEventPayload measures the shared custom boss-bar packet encoder.
func BenchmarkBossEventPayload(b *testing.B) {
	bar := world.BossBar{Wire: uuid.New(), Name: nbt.String("Boss"), Value: 25, Max: 100}
	b.ReportAllocs()
	for b.Loop() {
		_ = bossEventPayload(bar, protocol.BossEventAdd)
	}
}
