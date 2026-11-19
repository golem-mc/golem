package mobs

import (
	"math"
	"slices"
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

// TestAPillagerSparesAVindicator: illagers take no side against each
// other.
func TestAPillagerSparesAVindicator(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	p := srv.SpawnMob(pillagerKind, 60.5, 64, 60.5)
	v := srv.SpawnMob(vindicatorKind, 62.5, 64, 60.5)
	pumpTicks(c, srv, 200, servertest.HostileSkip, func(int32, []byte) bool { return false })
	if hp, _ := srv.MobHealth(v); hp != 24 {
		t.Errorf("vindicator health = %v after 200 ticks beside a pillager, want 24", hp)
	}
	if hp, _ := srv.MobHealth(p); hp != 24 {
		t.Errorf("pillager health = %v, want 24", hp)
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

// pillagerShotCrossbow runs a pillager armed with a crossbow carrying the
// given wear until its first arrow flies, and returns the crossbow it holds
// then and whether the main hand's break event reached the client.
func pillagerShotCrossbow(t *testing.T, damage int32) (protocol.ItemStack, bool) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 6.5, 64, 4.5)
	crossbow, _ := registry.ItemID("minecraft:crossbow")
	srv.SetMobHand(id, protocol.ItemStack{ItemID: crossbow, Count: 1, Damage: damage})
	var shot, broke bool
	var held protocol.ItemStack
	watch := func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusBreakMainHand {
				broke = true
			}
		}
		if !shot && len(srv.ArrowsInDimension(world.DimensionName)) > 0 {
			shot, held = true, srv.MobHand(id)
		}
		return shot && broke
	}
	pumpTicks(c, srv, 600, servertest.HostileSkip, watch)
	pumpTicks(c, srv, 3, servertest.HostileSkip, watch)
	if !shot {
		t.Fatal("the pillager never shot")
	}
	return held, broke
}

// TestAPillagersShotWearsItsCrossbow: each shot costs the crossbow a point of
// durability.
// vanilla: ProjectileWeaponItem.shoot, CrossbowItem.getDurabilityUse
func TestAPillagersShotWearsItsCrossbow(t *testing.T) {
	t.Parallel()
	hand, broke := pillagerShotCrossbow(t, 10)
	if hand.Damage != 11 || broke {
		t.Errorf("crossbow damage %d broke=%v after one shot from 10, want 11 and unbroken", hand.Damage, broke)
	}
}

// TestAPillagersLastShotBreaksItsCrossbow: the shot that wears the crossbow
// through leaves the hand empty and plays the main hand's break.
func TestAPillagersLastShotBreaksItsCrossbow(t *testing.T) {
	t.Parallel()
	crossbow, _ := registry.ItemID("minecraft:crossbow")
	hand, broke := pillagerShotCrossbow(t, registry.MaxDamage(crossbow)-1)
	if !hand.Empty() || !broke {
		t.Errorf("hand %+v broke=%v after the last shot, want empty and a break event", hand, broke)
	}
}

// TestAPillagersLoadRidesOnItsCrossbow: the charge loads an arrow into the
// crossbow itself, so watchers are sent the loaded crossbow (the client's
// hold pose) and the shot sends it empty again.
// vanilla: CrossbowItem.tryLoadProjectiles, LivingEntity.detectEquipmentUpdates
func TestAPillagersLoadRidesOnItsCrossbow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 6.5, 64, 4.5)
	arrow, _ := registry.ItemID("minecraft:arrow")
	var loadedSent, emptiedSent bool
	pumpTicks(c, srv, 600, nil, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		for {
			raw := r.ReadU8()
			stack, _ := r.ReadSlot()
			if raw&0x7F == 0 {
				loaded := stack.ChargedProjectiles()
				switch {
				case len(loaded) == 1 && loaded[0].ItemID == arrow:
					loadedSent = true
				case len(loaded) == 0 && loadedSent:
					emptiedSent = true
				}
			}
			if raw&0x80 == 0 || r.Err() != nil {
				break
			}
		}
		return emptiedSent
	})
	if !loadedSent {
		t.Fatal("no equipment packet showed the pillager's crossbow loaded with an arrow")
	}
	if !emptiedSent {
		t.Error("no equipment packet showed the crossbow emptied by the shot")
	}
}

// TestSpawnedPillagersAreSometimesCaptains: about 6% of pillagers spawned
// outside a raid, patrol or structure lead with the ominous banner.
// vanilla: PatrollingMonster.finalizeSpawn
func TestSpawnedPillagersAreSometimesCaptains(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	banner := srv.OminousBanner()
	captains := 0
	for seed := int64(1); seed <= 200; seed++ {
		id := srv.SpawnMobSeeded(pillagerKind, 40.5, 64, 40.5, seed)
		if srv.MobHeadStack(id).Equal(banner) {
			captains++
		}
	}
	if captains < 3 || captains > 30 {
		t.Errorf("captains = %d of 200, want about 12", captains)
	}
	if got := srv.MobHeadStack(srv.SpawnCaptain(pillagerKind, 40.5, 64, 40.5)); !got.Equal(banner) {
		t.Errorf("SpawnCaptain head = %v, want the banner", got)
	}
}

// TestAPillagerChargesWithItsCrossbowsQuickChargeSounds: a pillager holding a
// quick charge II crossbow plays that enchantment's start sound, not the
// default one.
// vanilla: CrossbowItem.onUseTick, getChargingSounds
func TestAPillagerChargesWithItsCrossbowsQuickChargeSounds(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(pillagerKind, 6.5, 64, 4.5)
	srv.SetMobEquipment(id, "mainhand", servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:quick_charge", 2))
	var heard []string
	pumpTicks(c, srv, 200, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySoundEffect {
			return false
		}
		if s := servertest.DecodePlaySoundEffect(t, data); s.RegistryID >= 0 {
			name, _ := servertest.SoundEventName(s.RegistryID)
			heard = append(heard, name)
			return name == "minecraft:item.crossbow.quick_charge_2"
		}
		return false
	})
	if slices.Contains(heard, "minecraft:item.crossbow.quick_charge_2") {
		return
	}
	t.Errorf("sounds heard %v, want item.crossbow.quick_charge_2", heard)
}
