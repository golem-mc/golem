package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob simulation's freeze, as ordinary unit tests: the counter the
// powder snow runs on a body whose own box shares a cell with it, the
// kinds the snow holds no hold on, and the hurts of the frozen. The
// black-box tests in internal/server/*_test.go remain the proof of a mob the server
// actually spawns and freezes.

// snowPit builds the standing powder snow a test freezes a body in: a
// two-deep pillar of it on a stone floor, the cells the body the test
// stands at suffuses.
func snowPit(t *testing.T) testWorld {
	t.Helper()
	snow := defaultState(t, "minecraft:powder_snow")
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	for y := int32(64); y <= 65; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				w.set(x, y, z, snow)
			}
		}
	}
	return w
}

// TestACowInPowderSnowFreezesAndThaws runs the counter on a body the snow
// holds: a tick up a tick while the body stands in it, two back down a tick
// out of it — no bite through the thaw, the counter leaving the full freeze
// the tick the body steps out.
func TestACowInPowderSnowFreezesAndThaws(t *testing.T) {
	w := snowPit(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.NoAI = true

	cow.Tick(w, nil)
	if cow.TicksFrozen != 1 {
		t.Fatalf("the first tick in the snow left the counter at %d, want 1", cow.TicksFrozen)
	}
	for range rules.FullyFrozenTicks - 1 {
		cow.Tick(w, nil)
	}
	if cow.TicksFrozen != rules.FullyFrozenTicks {
		t.Fatalf("the full stand left the counter at %d, want %d", cow.TicksFrozen, rules.FullyFrozenTicks)
	}
	if cow.Health != 10 {
		t.Fatalf("the freeze of a full counter took health, want the whole ten")
	}

	// The counter stops at the full freeze: the FREEZE effect's min holds
	// a body that stands on past it.
	for range 5 {
		cow.Tick(w, nil)
	}
	if cow.TicksFrozen != rules.FullyFrozenTicks {
		t.Fatalf("standing on past the freeze left the counter at %d, want the %d cap", cow.TicksFrozen, rules.FullyFrozenTicks)
	}

	// Out of the snow: two a tick down to nought, no bite on the way.
	dry := newTestWorld()
	floor(dry, -2, -2, 2, 2, 63, defaultState(t, "minecraft:stone"))
	for i := 1; i <= rules.FullyFrozenTicks/rules.FreezeThawTicks; i++ {
		for _, ev := range cow.Tick(dry, nil) {
			if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeFreeze {
				t.Fatalf("the thaw's tick %d bit on the way down, want none", i)
			}
		}
		if want := rules.FullyFrozenTicks - i*rules.FreezeThawTicks; cow.TicksFrozen != want {
			t.Fatalf("the thaw's tick %d left the counter at %d, want %d", i, cow.TicksFrozen, want)
		}
	}
}

// TestAStrayIsHeldByNothingInPowderSnow checks the kinds on vanilla's
// freeze_immune_entity_types tag among the herd this package runs: the
// stray never counts a frozen tick, and the freeze_immune_wearables spare
// nothing held in the hands, which are no armour slot.
func TestAStrayIsHeldByNothingInPowderSnow(t *testing.T) {
	w := snowPit(t)
	stray := aimless(NewMob(TypeStray, 1, 0.5, 64, 0.5, 1))
	stray.NoAI = true
	for range rules.FullyFrozenTicks + 2*rules.FreezeHurtTicks {
		stray.Tick(w, nil)
	}
	if stray.TicksFrozen != 0 {
		t.Errorf("the stray counted %d frozen ticks in the snow, want the kind the snow has no hold on", stray.TicksFrozen)
	}
	if stray.Health != stray.MaxHealth {
		t.Errorf("the stray in the snow lost health, want the kind the freeze spares")
	}

	// The herd's own kinds on the tag, and the freeze_hurts_extra ones:
	// what the registry names among the kinds here is the stray, the
	// polar bear, the wither and the snow golem — of the powder snow it is
	// built of, on the tag — spared, and the magma cube alone,
	// hurt for five.
	for i, k := range kinds {
		if k == nil {
			continue
		}
		kind := MobType(i)
		id, ok := registry.EntityTypeID(kind.Name())
		if !ok {
			continue
		}
		immune := registry.EntityTypeInTag(id, "minecraft:freeze_immune_entity_types")
		if immune && kind != TypeStray && kind != TypeSnowGolem && kind != TypePolarBear && kind != TypeWither {
			t.Errorf("%s is on the freeze_immune_entity_types tag, want the stray, the snow golem, the polar bear and the wither among this herd alone", kind)
		}
		if registry.EntityTypeInTag(id, "minecraft:freeze_hurts_extra_types") {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			m.Health = 100
			m.HurtByWorld(1, rules.DamageTypeFreeze)
			if m.Health != 95 {
				t.Errorf("%s is on the freeze_hurts_extra_types tag; health %v after one freeze damage, want 95", kind, m.Health)
			}
		}
	}

	// A mob's hands are no armour slot, so the leather the
	// freeze_immune_wearables name held there never stays its freeze off: it
	// counts as a bare body does.
	boots, _ := registry.ItemID("minecraft:leather_boots")
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.NoAI = true
	cow.MainHand = protocol.ItemStack{ItemID: boots, Count: 1}
	for range rules.FullyFrozenTicks {
		cow.Tick(w, nil)
	}
	if cow.TicksFrozen != rules.FullyFrozenTicks {
		t.Errorf("a cow holding leather boots counted %d frozen ticks, want the full %d: the hands are no armour slot", cow.TicksFrozen, rules.FullyFrozenTicks)
	}
}

// TestWornFireProtectionShortensAMobsBurn checks burning_time on a mob's
// ignition: Fire Protection IV boots turn 160 ticks into 64.
// vanilla: LivingEntity.igniteForTicks
func TestWornFireProtectionShortensAMobsBurn(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	id, _ := registry.ItemID("minecraft:diamond_boots")
	ench, _ := registry.EnchantmentID("minecraft:fire_protection")
	cow.Worn[0] = protocol.ItemStack{ItemID: id, Count: 1, Enchantments: map[int32]int32{ench: 4}}
	cow.IgniteForTicks(160)
	if cow.FireTicks != 64 {
		t.Errorf("the cow burns for %d ticks, want 64", cow.FireTicks)
	}
}
