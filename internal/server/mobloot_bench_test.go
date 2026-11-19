package server

import (
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobEquipmentPickup runs the complete loot pass with 64 actual
// equipment takes per iteration, including deferred sound/game-event dispatch.
// Movement and network clients are excluded; resetting equipment and ground
// stacks is timed equally on both revisions.
func BenchmarkMobEquipmentPickup(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	for _, tc := range []struct {
		name, slot string
		item       registry.Item
		head       protocol.ItemStack
	}{
		{name: "Helmet", slot: "head", item: registry.ItemIronHelmet},
		{name: "Sword", slot: "mainhand", item: registry.ItemIronSword},
		{name: "HelmetInHand", slot: "mainhand", item: registry.ItemIronHelmet,
			head: protocol.ItemStack{ItemID: int32(registry.ItemDiamondHelmet), Count: 1}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := config.Default()
			cfg.LevelName, cfg.LevelType = b.TempDir(), TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			dim := s.allDims()[0]
			for i := range 64 {
				x, z := float64(i%8)*8, float64(i/8)*8
				m := entity.NewMob(entity.TypeZombie, s.nextEntity(), x, 64, z, int64(i+1))
				m.CanPickUpLoot, m.NoAI = true, true
				s.addMob(&mobEntity{mob: m, w: dim.w, uuid: uuid.New()})
				s.spawnItem(dim.w, protocol.ItemStack{ItemID: int32(tc.item), Count: 1}, x, 64, z, 0, 0, 0, 0)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, m := range dim.mobs.herd {
					m.MainHand = protocol.ItemStack{}
					m.Worn = [4]protocol.ItemStack{}
					m.Worn[3] = tc.head
				}
				for _, it := range s.items {
					it.item.Dead, it.item.Stack.Count = false, 1
				}
				s.tickMobLoot()
			}
			b.StopTimer()
			for _, m := range dim.mobs.herd {
				stack := m.MainHand
				if tc.slot == "head" {
					stack = m.Worn[3]
				}
				if stack.ItemID != int32(tc.item) || stack.Count != 1 {
					b.Fatalf("mob %d slot %s = %+v, want one %v", m.ID, tc.slot, stack, tc.item)
				}
			}
			for _, it := range s.items {
				if !it.item.Dead || !it.item.Stack.Empty() {
					b.Fatal("equipment pickup left a ground stack")
				}
			}
			b.ReportMetric(64, "pickups/op")
		})
	}
}
