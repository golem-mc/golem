package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
	var flagged, shot bool
	var crossbowHit bool
	var pierce int32
	pumpTicks(c, srv, 600, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				if entries, ok := servertest.ReadMetadata(r); ok {
					if e := servertest.MetaOf(entries, protocol.MetadataIndexPillagerCharging); e != nil && e.FlagVal {
						flagged = true
					}
				}
			}
		}
		if arrows := srv.ArrowsInDimension(world.DimensionName); len(arrows) > 0 && !shot {
			shot = true
			crossbowHit, pierce = arrows[0].CrossbowHit, arrows[0].PierceLevel
		}
		return flagged && shot
	})
	if !flagged {
		t.Error("no metadata set the pillager's charging flag (index 17)")
	}
	if !shot {
		t.Fatal("the pillager never shot")
	}
	if !crossbowHit || pierce != 3 {
		t.Errorf("arrow crossbowHit=%v pierce=%d, want true and 3", crossbowHit, pierce)
	}
}
