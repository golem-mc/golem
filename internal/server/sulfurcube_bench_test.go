package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkSulfurCubeLoot isolates the shared sensing/pickup pass from movement.
// Slime geometry backs the sulfur-typed bodies on both revisions so the same
// fixture runs on main, which does not yet register sulfur cubes. Reset costs
// are included equally on both sides; every iteration starts empty and due.
func BenchmarkSulfurCubeLoot(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	for _, height := range []int{4, 0} {
		name := "Sense"
		if height == 0 {
			name = "Pickup"
		}
		b.Run(name, func(b *testing.B) {
			cfg := config.Default()
			cfg.LevelName, cfg.LevelType = b.TempDir(), TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			dim := s.allDims()[0]
			stone, _ := registry.ItemID("minecraft:stone")
			stick, _ := registry.ItemID("minecraft:stick")
			for i := range 64 {
				x, z := float64(i%8)*8, float64(i/8)*8
				m := entity.NewMob(entity.TypeSlime, int32(i+1), x, 64, z, int64(i+1))
				m.SetSize(2, true)
				m.Type, m.CanPickUpLoot = registry.EntityTypeSulfurCube, true
				dim.mobs.herd = append(dim.mobs.herd, m)
			}
			for i := range 256 {
				x, z := float64(i%8)*8, float64(i/8%8)*8
				item := stone
				if i >= 64 {
					x, z = x+3, z+3
					if i%2 == 0 {
						item = stick
					}
				}
				// No item ticking: these positions and pickup delays stay fixed.
				s.spawnItem(dim.w, protocol.ItemStack{ItemID: item, Count: 1}, x, 64+float64(height), z, 0, 0, 0, 0)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, m := range dim.mobs.herd {
					m.Body, m.MainHand = protocol.ItemStack{}, protocol.ItemStack{}
				}
				for _, it := range s.items {
					it.item.Dead, it.item.Stack.Count = false, 1
				}
				s.tickMobLoot()
			}
			b.StopTimer()
			filled := 0
			for _, m := range dim.mobs.herd {
				if !m.Body.Empty() {
					filled++
				}
			}
			b.ReportMetric(64, "mobs")
			b.ReportMetric(256, "items")
			b.ReportMetric(float64(filled), "filled")
			if height == 4 && filled != 0 {
				b.Fatalf("distant items filled %d cubes", filled)
			}
		})
	}
}
