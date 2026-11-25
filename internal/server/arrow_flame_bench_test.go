package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkFlameArrowFirstTick fires fifty Flame arrows at a watching player
// and runs their first tracking update; the previous batch is cleared
// between iterations, off the clock.
func BenchmarkFlameArrowFirstTick(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, 1)
	c := clients[0]
	flame, ok := registry.EnchantmentID("minecraft:flame")
	if !ok {
		b.Fatal("the registry has no flame")
	}
	bow := protocol.ItemStack{ItemID: int32(registry.ItemBow), Count: 1, Enchantments: map[int32]int32{flame: 1}}
	ammo := protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 1}
	pos := c.player.Position()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		s.arrowsMu.Lock()
		for _, it := range s.arrows {
			it.arrow.Dead = true
		}
		s.arrowsMu.Unlock()
		s.tickArrows()
		b.StartTimer()
		for range 50 {
			c.spawnBowArrow(pos, bow, ammo, 1, entity.PickupAllowed)
		}
		s.tickArrows()
	}
	b.StopTimer()
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	if len(s.arrows) != 50 || s.arrows[0].arrow.FireTicks == 0 {
		b.Fatalf("%d arrows, want 50 burning", len(s.arrows))
	}
}
