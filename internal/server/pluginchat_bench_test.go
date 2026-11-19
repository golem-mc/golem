package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/google/uuid"
)

// BenchmarkTypedChatRelay includes decoding, hook gates, encoding and recipient queueing.
func BenchmarkTypedChatRelay(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(old) })
	for _, enabled := range []bool{false, true} {
		name := "no-plugins"
		if enabled {
			name = "no-listeners"
		}
		b.Run(name, func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.ChatSpamThresholdSeconds = 0
			s := &Server{Config: cfg, clients: make(map[uuid.UUID]*client), scoreboard: scoreboard.New(nil)}
			if enabled {
				s.plugins = pluginruntime.New(nil, nil)
			}
			s.deferWakes.Store(true)
			id := protocol.OfflineUUID("Bench")
			c := &client{srv: s, log: slog.Default(), profile: player.Profile{UUID: id, Name: "Bench"}, player: &player.Player{UUID: id, Name: "Bench"}, chatReady: true}
			c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
			s.assignPlayerSlot(c)
			s.clients[id] = c
			var payload protocol.Buffer
			payload.PutString("hello")
			payload.PutI64(0)
			payload.PutI64(0)
			payload.PutBool(false)
			payload.PutVarInt(0)
			payload.PutRaw([]byte{0, 0, 0, 1})
			b.ReportAllocs()
			for b.Loop() {
				if err := c.handleChat(protocol.NewReader(payload.Bytes())); err != nil {
					b.Fatal(err)
				}
				c.out.queue = c.out.queue[:0]
				c.out.queued = 0
			}
		})
	}
}
