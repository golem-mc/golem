package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkSulfurCubeTick steps 64 filled cubes and eight touching players.
// Every iteration resets motion, contact damage windows and player movement so
// movement, hot contact and unpowered TNT polling stay active throughout.
func BenchmarkSulfurCubeTick(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	cfg := config.Default()
	cfg.LevelName, cfg.LevelType = b.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	var holds []world.ChunkPos
	for x := range int32(2) {
		for z := range int32(2) {
			pos := world.ChunkPos{X: x, Z: z}
			dim.w.Acquire(pos)
			holds = append(holds, pos)
		}
	}
	b.Cleanup(func() {
		clear(s.clients)
		for _, pos := range holds {
			dim.w.Release(pos)
		}
		s.Shutdown()
	})
	dim.w.FlushLighting()
	var cubes []*entity.Mob
	for i := range 64 {
		x, z := 2.5+float64(i%8)*3, 2.5+float64(i/8)*3
		id := s.spawnMobInDim(dim, entity.TypeSulfurCube, x, 64, z, int64(i+1), nil)
		m := s.mobByEntityIDLocked(dim.w, id).mob
		name := "minecraft:oak_planks"
		if i < 8 {
			name = "minecraft:magma_block"
		} else if i%2 == 0 {
			name = "minecraft:tnt"
		}
		item, _ := registry.ItemID(name)
		m.Body = protocol.ItemStack{ItemID: item, Count: 1}
		m.Persistence = true
		cubes = append(cubes, m)
		if i < 8 {
			p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: x - 0.6, Y: 64, Z: z, OnGround: true})
			c := &client{srv: s, player: p}
			c.dim.Store(dim)
			s.assignPlayerSlot(c)
			s.clients[p.UUID] = c
		}
	}
	step := func() {
		for i, m := range cubes {
			m.X, m.Y, m.Z = 2.5+float64(i%8)*3, 64.2, 2.5+float64(i/8)*3
			m.VX, m.VY, m.VZ = 0.15, -0.3, 0.1
		}
		for _, c := range s.clients {
			c.player.UpdateVitals(func(v *player.Vitals) { v.Health, v.Dead, v.InvulnerableTicks, v.LastHurt = 20, false, 0, 0 })
			c.player.SetKnownMovement(player.Velocity{X: 0.1})
		}
		s.tickMobs()
	}
	step()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step()
	}
	b.StopTimer()
	b.ReportMetric(64, "cubes")
	b.ReportMetric(8, "players")
}
