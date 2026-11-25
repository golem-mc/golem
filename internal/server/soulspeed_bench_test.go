package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickAttributes steps one walking player's attribute tick, with and
// without Soul Speed boots or a Speed effect on soul sand, moving a block each tick.
func BenchmarkTickAttributes(b *testing.B) {
	for _, tc := range []struct {
		name      string
		enchanted bool
		speed     bool
	}{{"plain", false, false}, {"soul_speed", true, false}, {"speed_effect", false, true}} {
		b.Run(tc.name, func(b *testing.B) {
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
			pos := world.ChunkPos{}
			dim.w.Acquire(pos)
			b.Cleanup(func() {
				clear(s.clients)
				dim.w.Release(pos)
				s.Shutdown()
			})
			dim.w.FlushLighting()
			sand, _ := registry.FindState("minecraft:soul_sand", nil)
			for x := int32(1); x <= 3; x++ {
				dim.w.SetBlock(x, 63, 1, sand)
			}
			p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: 1.5, Y: 64, Z: 1.5, OnGround: true})
			boots, _ := registry.ItemID("minecraft:diamond_boots")
			soulSpeed, _ := registry.EnchantmentID("minecraft:soul_speed")
			giveBoots := func() {
				p.Inventory(func(inv *player.Inventory) {
					inv.Set(player.ArmorSlot+3, protocol.ItemStack{ItemID: boots, Count: 1, Enchantments: map[int32]int32{soulSpeed: 3}})
				})
			}
			if tc.speed {
				p.UpdateBody(func(_ *player.Vitals, e *effect.Set, _ *player.Attributes) {
					e.Add(effect.Instance{ID: registry.MobEffectSpeed, Amplifier: 1, Duration: 1 << 30})
				})
			}
			c := &client{srv: s, player: p}
			c.dim.Store(dim)
			s.assignPlayerSlot(c)
			s.clients[p.UUID] = c
			step := func(i int) {
				if tc.enchanted && i%256 == 0 { // wear would break the boots
					giveBoots()
				}
				x := 1.5 + float64((i+1)%2)
				p.SetPosition(player.Position{X: x, Y: 64, Z: 1.5, OnGround: true})
				p.SetKnownMovement(player.Velocity{X: 1 - 2*float64(i%2)})
				p.UpdateVitals(func(v *player.Vitals) { v.AgeTicks = i })
				c.tickAttributes()
			}
			step(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				step(i + 1)
			}
		})
	}
}
