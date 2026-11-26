package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkArrowPlayerOutcome measures a landed player-owned arrow against
// unenchanted armour, including the victim's post-attack equipment scan.
func BenchmarkArrowPlayerOutcome(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, 2)
	shooter, victim := clients[0], clients[1]
	for _, c := range clients {
		c.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
	}
	victim.player.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.ArmorSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemDiamondChestplate), Count: 1, Unbreakable: true}
	})
	for _, tc := range []struct {
		name    string
		shooter int32
	}{{"owned", shooter.player.EntityID}, {"ownerless", 0}} {
		b.Run(tc.name, func(b *testing.B) {
			arrow := newArrowEntity(s, s.world, 0.5, 65, 0.5, 1, 0, 0, 0, 0,
				false, entity.ArrowBaseDamage, entity.PickupDisallowed, tc.shooter,
				shooter.player.UUID, protocol.ItemStack{}, 0)
			hit := arrowPlayerHit{arrow: arrow, w: s.world, victim: victim, damage: 1, vx: 1}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				victim.player.UpdateVitals(func(v *player.Vitals) {
					v.Health = 20
					v.InvulnerableTicks = 0
				})
				s.arrowPlayerOutcome(hit)
			}
			b.StopTimer()
			if health := victim.player.Vitals().Health; health >= 20 {
				b.Fatalf("the arrow never hurt the victim: health %v", health)
			}
		})
	}
}
