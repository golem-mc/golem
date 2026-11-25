package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickFireworksInHerd ticks ten crossbow rockets above a flat world's herd.
func BenchmarkTickFireworksInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(strconv.Itoa(cows), func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			dim := s.allDims()[0]
			w := dim.w
			for x := int32(-4); x < 4; x++ {
				for z := int32(-4); z < 4; z++ {
					b.Cleanup(w.HoldEntityTicking(world.ChunkPos{X: x, Z: z}))
				}
			}
			w.FlushLighting()

			for i := range cows {
				x, z := float64(i%50)*2-49.5, float64(i/50)*2-39.5
				s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i+1), nil)
			}
			for x := range 10 {
				s.SpawnFireworkAtAngle(float64(x), world.GroundLevel+20, 0, 0, 0, 0, 1<<30)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickFireworks()
			}
			b.StopTimer()
			if len(s.fireworks) != 10 || s.fireworks[0].rocket.Life != b.N {
				b.Fatal("the rockets did not tick throughout the run")
			}
		})
	}
}

// BenchmarkUpdateFallFlying measures a gentle glide's per-tick checks between
// the ten-tick game events and twenty-tick equipment wear.
func BenchmarkUpdateFallFlying(b *testing.B) {
	p := &player.Player{}
	p.SetPosition(player.Position{Y: 90})
	p.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.ArmorSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemElytra), Count: 1}
	})
	c := &client{player: p}
	c.fallFlying.Store(true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p.UpdateVitals(func(v *player.Vitals) { v.FallDistance = 1.3 })
		c.fallFlyTicks = 0
		c.updateFallFlying()
	}
}
