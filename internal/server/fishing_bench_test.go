package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickBobbersInHerd ticks one fresh cast beside a flat world's herd.
func BenchmarkTickBobbersInHerd(b *testing.B) {
	for _, count := range []int{0, 2000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
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

			for i := range count {
				x, z := float64(i%50)*2-49.5, float64(i/50)*2-39.5
				s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i+1), nil)
				s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1}, x, world.GroundLevel, z, 0, 0, 0, 0)
			}
			// Cast along the lane between rows so even the populated hook stays in flight.
			p := &player.Player{EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: 7.5, Y: world.GroundLevel, Z: 7.5})
			p.Inventory(func(inv *player.Inventory) {
				inv.Slots[player.HotbarSlot] = protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1}
			})
			c := &client{srv: s, player: p}
			c.dim.Store(dim)
			s.assignPlayerSlot(c)
			s.clients[uuid.Nil] = c
			b.Cleanup(func() { delete(s.clients, uuid.Nil) })

			// Measure only the hook tick. Pausing the benchmark timer for
			// each cast would force several heap statistics reads per tick.
			// The allocation counters cover setup and teardown as well.
			b.ReportAllocs()
			var tickTime time.Duration
			for b.Loop() {
				hook := entity.NewBobber(s.nextEntity(), 7.5, world.GroundLevel+1.62, 7.5, 0, 0, p.EntityID, 1, 0, 0)
				it := newBobberEntity(hook, w, uuid.Nil)
				s.registerBobber(it)
				start := time.Now()
				s.tickBobbers()
				tickTime += time.Since(start)
				if hook.TickCount != 1 || hook.Dead || hook.HookedID != 0 {
					b.Fatal("the cast did not tick in free flight")
				}
				s.takeBobber(it)
				s.destroyed = s.destroyed[:0]
			}
			b.ReportMetric(float64(tickTime)/float64(b.N), "ns/op")
		})
	}
}
