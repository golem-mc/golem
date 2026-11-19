package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkEnchantedMobBlow measures the mob tick's landingBlow through hurt
// and armourAbsorb, with Breach IV on the attacker and full iron on the player.
func BenchmarkEnchantedMobBlow(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, 1)
	victim := clients[0]
	victim.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
	victim.player.Inventory(func(inv *player.Inventory) {
		for i, item := range []registry.Item{registry.ItemIronHelmet, registry.ItemIronChestplate, registry.ItemIronLeggings, registry.ItemIronBoots} {
			inv.Slots[player.ArmorSlot+i] = protocol.ItemStack{ItemID: int32(item), Count: 1, Unbreakable: true}
		}
	})
	breach, ok := registry.EnchantmentID("minecraft:breach")
	if !ok {
		b.Fatal("missing Breach")
	}
	mob := entity.NewMob(entity.TypeZombie, s.nextEntity(), 2, 64, 0, 1)
	mob.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemMace), Count: 1, Enchantments: map[int32]int32{breach: 4}}
	strike := mobStrike{w: s.world, mob: mob, victimID: victim.player.EntityID, damage: 6, x: 2, y: 64,
		killer: &rules.Killer{Weapon: mob.MainHand}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		victim.player.UpdateVitals(func(v *player.Vitals) {
			v.Health = 20
			v.InvulnerableTicks = 0
		})
		s.landingBlow(strike, clients)
	}
	b.StopTimer()
	if health := victim.player.Vitals().Health; health >= 20 {
		b.Fatalf("mob blow never reached the player: health %v", health)
	}
}
