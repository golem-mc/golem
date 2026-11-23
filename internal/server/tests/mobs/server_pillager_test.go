package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const pillagerKind = "minecraft:pillager"

// TestAPillagerIsBornWithACrossbow: a spawned pillager holds a crossbow and
// has 24 health.
// vanilla: Pillager.populateDefaultEquipmentSlots, createAttributes
func TestAPillagerIsBornWithACrossbow(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 40.5, 64, 40.5)
	crossbow, _ := registry.ItemID("minecraft:crossbow")
	if got := srv.MobHand(id); got.ItemID != crossbow {
		t.Errorf("main hand = item %d, want the crossbow %d", got.ItemID, crossbow)
	}
	if hp, _ := srv.MobHealth(id); hp != 24 {
		t.Errorf("health = %v, want 24", hp)
	}
}

// TestAPillagerChargesAndShootsFromRange: a pillager inside its eight-block
// radius charges its crossbow, then hurts the player without closing to
// melee reach.
// vanilla: RangedCrossbowAttackGoal.tick
func TestAPillagerChargesAndShootsFromRange(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 6.5, 64, 4.5)
	var charged, hurt bool
	var hurtDist float64
	pumpTicks(c, srv, 600, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if ch, _, _ := srv.MobCrossbow(id); ch {
			charged = true
		}
		if pid == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				hurt = true
				x, _, z, _ := srv.MobPos(id)
				hurtDist = math.Hypot(x-0.5, z-0.5)
				return true
			}
		}
		return false
	})
	if !charged {
		t.Error("the pillager never charged its crossbow")
	}
	if !hurt {
		t.Fatal("the pillager never hurt the player")
	}
	if hurtDist < 3 {
		t.Errorf("the pillager was %.1f blocks away at the hit, want it shooting from range", hurtDist)
	}
}

// TestAPillagerShowsItsChargeAndShootsPiercingCrossbowArrows: the charging
// flag (metadata 17) reaches the client while the crossbow draws, and the
// arrow carries the crossbow hit sound and the weapon's piercing level.
// vanilla: Pillager.DATA_IS_CHARGING_CROSSBOW, CrossbowItem.createProjectile
func TestAPillagerShowsItsChargeAndShootsPiercingCrossbowArrows(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 6.5, 64, 4.5)
	crossbow, _ := registry.ItemID("minecraft:crossbow")
	piercing, _ := registry.EnchantmentID("minecraft:piercing")
	srv.SetMobHand(id, protocol.ItemStack{ItemID: crossbow, Count: 1, Enchantments: map[int32]int32{piercing: 3}})
	expectPillagerCrossbowCharge(t, c, srv, id, protocol.EntityFlagHandActive)
	expectPillagerCrossbowArrow(t, c, srv, 3)
}

func TestPillagerOffhandCrossbow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, main, off string
		handFlags       uint8
		pierce          int32
		chargeTicks     int
		mainDamage      int32
		offDamage       int32
		breaks          bool
	}{
		{
			name: "off hand only", handFlags: protocol.EntityFlagHandActive | protocol.EntityFlagActiveHandOff,
			off:    `{id:"minecraft:crossbow",count:1,components:{"minecraft:enchantments":{"minecraft:piercing":3,"minecraft:quick_charge":3}}}`,
			pierce: 3, offDamage: 1, chargeTicks: 10, // Quick Charge III: 1.25 s minus 0.75 s.
		},
		{
			name: "both hands", handFlags: protocol.EntityFlagHandActive,
			main:   `,mainhand:{id:"minecraft:crossbow",count:1,components:{"minecraft:enchantments":{"minecraft:piercing":2}}}`,
			off:    `{id:"minecraft:crossbow",count:1,components:{"minecraft:enchantments":{"minecraft:piercing":3}}}`,
			pierce: 2, mainDamage: 1,
		},
		{
			name: "off hand breaks", handFlags: protocol.EntityFlagHandActive | protocol.EntityFlagActiveHandOff,
			off:    `{id:"minecraft:crossbow",count:1,components:{"minecraft:enchantments":{"minecraft:piercing":3},"minecraft:max_damage":1}}`,
			pierce: 3, breaks: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Observer")
			servertest.HoldTicks(t, srv)
			srv.SetDifficulty(rules.DifficultyNormal)
			srv.RunLine(`summon minecraft:villager 2.5 64 0.5 {NoAI:1b}`)
			srv.RunLine(fmt.Sprintf(`summon minecraft:pillager 6.5 64 0.5 {PersistenceRequired:1b,equipment:{offhand:%s%s},attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`, tc.off, tc.main))
			var id, villager int32
			if !c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
				if pid != protocol.IDPlaySpawnEntity {
					return false
				}
				r := protocol.NewReader(data)
				entityID := r.ReadVarInt()
				r.ReadUUID()
				switch r.ReadVarInt() {
				case protocol.EntityTypeVillager:
					villager = entityID
				case protocol.EntityTypePillager:
					id = entityID
				}
				return id != 0 && villager != 0
			}) {
				t.Fatal("the summoned villager and pillager did not both spawn")
			}

			expectPillagerCrossbowCharge(t, c, srv, id, tc.handFlags)
			if tc.chargeTicks > 0 {
				servertest.RunTicks(t, srv, tc.chargeTicks-1)
				if len(srv.MobOffHand(id).ChargedProjectiles()) != 0 {
					t.Fatal("the off-hand crossbow loaded before its charge ended")
				}
				servertest.RunTicks(t, srv, 1)
				if got := srv.MobOffHand(id).ChargedProjectiles(); len(got) != 1 || registry.Item(got[0].ItemID) != registry.ItemArrow {
					t.Fatalf("off-hand crossbow held %v at the end of its charge, want one arrow", got)
				}
			}
			if tc.breaks {
				if !c.ReadTicks(srv, 80, func(pid int32, data []byte) bool {
					if pid != protocol.IDPlayEntityEvent {
						return false
					}
					r := protocol.NewReader(data)
					return r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusBreakOffHand
				}) {
					t.Fatal("no off-hand break event on the pillager's first shot")
				}
			}
			expectPillagerCrossbowArrow(t, c, srv, tc.pierce)
			main, off := srv.MobHand(id), srv.MobOffHand(id)
			if main.Empty() != (tc.main == "") || main.Damage != tc.mainDamage {
				t.Errorf("main hand = item %d with damage %d, want empty=%v and damage %d", main.ItemID, main.Damage, tc.main == "", tc.mainDamage)
			}
			if off.Empty() != tc.breaks || off.Damage != tc.offDamage {
				t.Errorf("off hand = item %d with damage %d, want empty=%v and damage %d", off.ItemID, off.Damage, tc.breaks, tc.offDamage)
			}
			servertest.UntilTicks(t, srv, 10, "the pillager's arrow to hurt the villager", func() bool {
				health, ok := srv.MobHealth(villager)
				return ok && health < 20
			})
		})
	}
}

func expectPillagerCrossbowCharge(t *testing.T, c *servertest.Client, srv *server.Server, id int32, handFlags uint8) {
	t.Helper()
	var hand uint8
	if !c.ReadTicks(srv, 60, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		entries, ok := servertest.ReadMetadata(r)
		if !ok {
			return false
		}
		if e := servertest.MetaOf(entries, protocol.MetadataIndexHandState); e != nil {
			hand = e.ByteVal
		}
		e := servertest.MetaOf(entries, protocol.MetadataIndexPillagerCharging)
		return e != nil && e.FlagVal
	}) {
		t.Fatal("no metadata set the pillager's charging flag within 60 ticks")
	}
	if hand != handFlags {
		t.Errorf("charging hand flags = %#x, want %#x", hand, handFlags)
	}
}

func expectPillagerCrossbowArrow(t *testing.T, c *servertest.Client, srv *server.Server, pierce int32) {
	t.Helper()
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) == 0 {
		if !servertest.ASpawnOfWithinTicks(c, srv, 80, protocol.EntityTypeArrow) {
			t.Fatal("the pillager never shot within 80 ticks of charging")
		}
		arrows = srv.ArrowsInDimension(world.DimensionName)
	}
	if len(arrows) != 1 {
		t.Fatalf("pillager arrows = %d, want one first shot", len(arrows))
	}
	if a := arrows[0]; !a.CrossbowHit || a.PierceLevel != pierce {
		t.Errorf("arrow crossbowHit=%v pierce=%d, want true and %d", a.CrossbowHit, a.PierceLevel, pierce)
	}
}
