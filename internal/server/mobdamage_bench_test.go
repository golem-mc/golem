package server

import (
	"strconv"
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

// BenchmarkMobContactStrike measures landingBlow for a slime's touch on a
// player and on a cow, with the victim's team allowing or refusing the damage
// (friendlyFire on or off).
func BenchmarkMobContactStrike(b *testing.B) {
	for _, victimKind := range []string{"player", "sting", "mob"} {
		for _, refused := range []bool{false, true} {
			name := victimKind + "/allowed"
			if refused {
				name = victimKind + "/refused"
			}
			b.Run(name, func(b *testing.B) {
				s, clients := newIdlePlayersBenchServer(b, 1)
				player0 := clients[0]
				player0.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
				dim := s.allDims()[0]
				slimeID := s.spawnMobInDim(dim, entity.TypeSlime, 2, 64, 0, 1, nil)
				cowID := s.spawnMobInDim(dim, entity.TypeCow, 4, 64, 0, 2, nil)
				s.mobsMu.Lock()
				slime := s.mobByEntityIDLocked(s.world, slimeID)
				cow := s.mobByEntityIDLocked(s.world, cowID)
				s.mobsMu.Unlock()
				cowHealth := cow.mob.Health
				s.RunLine("team add t")
				s.RunLine("team modify t friendlyFire " + strconv.FormatBool(!refused))
				s.RunLine("team join t " + slime.uuid.String())
				member := player0.player.Name
				strike := mobStrike{w: s.world, mob: slime.mob, uuid: slime.uuid, uuidText: slime.uuidText, damage: 2, x: 2, y: 64,
					touch: true, killer: &rules.Killer{}}
				if victimKind == "sting" {
					strike.puff = 2
				}
				if victimKind != "mob" {
					strike.victimID = player0.player.EntityID
				} else {
					strike.victimID = cow.mob.ID
					member = cow.uuidText
				}
				s.RunLine("team join t " + member)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					player0.player.UpdateVitals(func(v *player.Vitals) {
						v.Health = 20
						v.InvulnerableTicks = 0
					})
					s.mobsMu.Lock()
					cow.mob.Health, cow.mob.HurtTime, cow.mob.InvulnTicks = cowHealth, 0, 0
					s.mobsMu.Unlock()
					s.landingBlow(strike, clients)
					if victimKind == "sting" {
						player0.stepVelocity(s.world, true) // the flush a sting leaves pending
					}
				}
				b.StopTimer()
				var hurt bool
				if victimKind != "mob" {
					hurt = player0.player.Vitals().Health < 20
				} else {
					s.mobsMu.Lock()
					hurt = cow.mob.Health < cowHealth
					s.mobsMu.Unlock()
				}
				if hurt == refused {
					b.Fatalf("victim hurt = %v with refused = %v", hurt, refused)
				}
			})
		}
	}
}
