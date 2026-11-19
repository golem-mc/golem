package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

// BenchmarkGameEvent measures player events and registered mob events in a populated herd.
func BenchmarkGameEvent(b *testing.B) {
	for _, name := range []string{"NoWarden", "OneWarden", "MobHerd"} {
		b.Run(name, func(b *testing.B) {
			logger := slog.Default()
			slog.SetDefault(slog.New(slog.DiscardHandler))
			b.Cleanup(func() { slog.SetDefault(logger) })
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			p := &player.Player{UUID: uuid.New(), Name: "Listener", EntityID: 1}
			p.SetPosition(player.Position{X: 0.5, Y: 64, Z: 0.5})
			c := &client{srv: s, player: p}
			c.dim.Store(s.allDims()[0])
			s.assignPlayerSlot(c)
			s.clients[p.UUID] = c
			b.Cleanup(func() { delete(s.clients, p.UUID) })
			ctx := rules.GameEventContext{Player: p.UUID}
			if name == "MobHerd" {
				for i := range 256 {
					s.spawnMobInDim(s.allDims()[0], entity.TypeCow, 0.5, 64, 0.5, int64(i), nil)
				}
				s.mobsMu.Lock()
				id := s.herdOf(s.world).MobUUID(s.herdOf(s.world).herd[0])
				s.mobsMu.Unlock()
				ctx = rules.GameEventContext{Mob: id}
			}
			if name != "NoWarden" {
				s.mobsMu.Lock()
				s.herdOf(s.world).add(entity.NewMob(entity.TypeWarden, 2, 4.5, 64, 0.5, 1))
				s.mobsMu.Unlock()
			}
			b.ReportAllocs()
			for b.Loop() {
				s.gameEvent(s.world, rules.GameEventStep, 0.5, 64, 0.5, ctx)
			}
		})
	}
}
