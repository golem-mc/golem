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

// TestAFrozenCowBitesEveryFortiethTickOfTheAge checks the hurts of the
// full freeze: the first on the body's next fortieth tick of age — twenty
// past the freeze itself — and one on every fortieth after, the bite
// following the age whatever the counter reads.
func TestAFrozenCowBitesEveryFortiethTickOfTheAge(t *testing.T) {
	w := snowPit(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.NoAI = true

	// The stand itself bites nobody, whatever else the body's own tick
	// does — its idle muttering among it.
	for range rules.FullyFrozenTicks {
		for _, ev := range cow.Tick(w, nil) {
			if ev.Kind == MobBurned {
				t.Fatalf("a tick of the freeze alone gave a bite of type %s, want no hurt before the counter is full", ev.DamageType)
			}
		}
	}
	var bites int
	for i := 1; i <= 2*rules.FreezeHurtTicks; i++ {
		for _, ev := range cow.Tick(w, nil) {
			if ev.Kind != MobBurned {
				continue // the idle mutter, no bite
			}
			if ev.DamageType != rules.DamageTypeFreeze {
				t.Fatalf("tick %d past the freeze bit with %s, want the freeze alone", i, ev.DamageType)
			}
			bites++
		}
		// The body's first fortieth past the freeze is the hundred and
		// sixtieth, twenty ticks in.
		want := (rules.FullyFrozenTicks+i)/rules.FreezeHurtTicks - rules.FullyFrozenTicks/rules.FreezeHurtTicks
		if bites != want {
			t.Fatalf("tick %d past the freeze had landed %d bites, want %d", i, bites, want)
		}
	}
	if want := 10.0 - float64(bites); !closeEnough(cow.Health, want) {
		t.Errorf("the frozen cow's health = %v, want %v", cow.Health, want)
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

// TestLeatherBootsStayAMobsFreezeOff checks the armour slot vanilla's
// LivingEntity.canFreeze walks for every living body: a zombie wearing
// leather boots never counts a frozen tick in the snow, where the same boots
// in its hands would not have spared it.
// vanilla: LivingEntity.canFreeze
func TestLeatherBootsStayAMobsFreezeOff(t *testing.T) {
	w := snowPit(t)
	boots, _ := registry.ItemID("minecraft:leather_boots")
	zombie := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	zombie.NoAI = true
	zombie.Worn[wornFeet] = protocol.ItemStack{ItemID: boots, Count: 1}
	for range rules.FullyFrozenTicks + 2*rules.FreezeHurtTicks {
		zombie.Tick(w, nil)
	}
	if zombie.TicksFrozen != 0 {
		t.Errorf("a zombie in leather boots counted %d frozen ticks, want the counter held at nought", zombie.TicksFrozen)
	}
	if zombie.Health != zombie.MaxHealth {
		t.Errorf("a zombie in leather boots lost health to the freeze, want the wearable to stay the whole of it")
	}
}

// TestIgniteForTicksThaws checks the thaw a flame brings: the burn rises to
// the ticks given and the frozen counter is wiped, while a burn already
// longer than the flame grants is left standing.
// vanilla: Entity.igniteForTicks
func TestIgniteForTicksThaws(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.TicksFrozen = rules.FullyFrozenTicks
	cow.IgniteForTicks(200)
	if cow.FireTicks != 200 {
		t.Errorf("the flame left the cow burning for %d ticks, want 200", cow.FireTicks)
	}
	if cow.TicksFrozen != 0 {
		t.Errorf("the flame left the cow %d frozen ticks, want the thaw to nothing", cow.TicksFrozen)
	}

	// A longer burn already standing is not cut short, and the thaw still
	// follows.
	cow.FireTicks = 300
	cow.TicksFrozen = rules.FullyFrozenTicks
	cow.IgniteForTicks(200)
	if cow.FireTicks != 300 {
		t.Errorf("a shorter flame cut the burn to %d, want the 300 left standing", cow.FireTicks)
	}
	if cow.TicksFrozen != 0 {
		t.Errorf("the flame left the cow %d frozen ticks, want the thaw to nothing", cow.TicksFrozen)
	}
}

// TestFreezeStepReadsTheGroundUnderFoot checks the frost slow's gate is read
// from the world: a body over a block is not over air, one with only air
// under its feet is.
// vanilla: LivingEntity.tryAddFrost
func TestFreezeStepReadsTheGroundUnderFoot(t *testing.T) {
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, defaultState(t, "minecraft:stone"))
	grounded := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	grounded.NoAI = true
	grounded.TicksFrozen = rules.FullyFrozenTicks
	grounded.freezeStep(w, &contactScan{}, nil)
	if grounded.underfootAir {
		t.Error("a cow over stone read air under its feet, want the ground it stands on")
	}

	bare := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	bare.NoAI = true
	bare.TicksFrozen = rules.FullyFrozenTicks
	bare.freezeStep(newTestWorld(), &contactScan{}, nil)
	if !bare.underfootAir {
		t.Error("a cow over air did not read air under its feet")
	}
}

// TestTheFrostSlowNeedsGroundUnderFoot checks the slow's air gate: a frozen
// body over a block takes the frozen share off its pace, one over air keeps
// the whole of it.
// vanilla: LivingEntity.tryAddFrost
func TestTheFrostSlowNeedsGroundUnderFoot(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	base := kinds[TypeCow].speed

	cow.TicksFrozen = rules.FullyFrozenTicks
	cow.underfootAir = false
	slowed := base + rules.FreezeSpeedAmount(rules.FullyFrozenTicks)
	if got := cow.pace(); !closeEnough(got, slowed) {
		t.Errorf("a frozen cow on the ground runs at %v, want the slowed %v", got, slowed)
	}

	cow.underfootAir = true
	if got := cow.pace(); !closeEnough(got, base) {
		t.Errorf("a frozen cow over air runs at %v, want the unslowed %v", got, base)
	}
}

// TestAFallingBodyInPowderSnowStillSlows checks the frost slow's gate for a
// body that is not on the ground: with no supporting block, vanilla reads the
// raw cell a fifth of a block below the feet, which inside a powder-snow
// column is the snow, so the slow stays on the body's pace.
// vanilla: LivingEntity.tryAddFrost, Entity.getOnPosLegacy
func TestAFallingBodyInPowderSnowStillSlows(t *testing.T) {
	w := snowPit(t)
	// The body stands in the pit's upper snow cell with the cell below it
	// also snow, so OnPos finds no collision to support it.
	cow := aimless(NewMob(TypeCow, 1, 0.5, 65, 0.5, 1))
	cow.NoAI = true
	cow.OnGround = false
	cow.TicksFrozen = rules.FullyFrozenTicks
	cow.freezeStep(w, &contactScan{}, nil)
	if cow.underfootAir {
		t.Fatal("a falling cow inside the snow read air under its feet, want the raw snow cell")
	}
	base := kinds[TypeCow].speed
	want := base + rules.FreezeSpeedAmount(rules.FullyFrozenTicks)
	if got := cow.pace(); !closeEnough(got, want) {
		t.Errorf("a falling frozen cow runs at %v, want the slowed %v", got, want)
	}
}

// TestTheFrostSlowLandsBeforeABabyZombiesHaste checks the modifier order
// AttributeInstance.calculateValue gives: the frost's ADD_VALUE comes off the
// base before the baby's ADD_MULTIPLIED_BASE haste scales it, so a frozen
// baby runs at (0.23 - 0.05) * 1.5 and not 0.23 * 1.5 - 0.05.
// vanilla: AttributeInstance.calculateValue
func TestTheFrostSlowLandsBeforeABabyZombiesHaste(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
	zombie.SetBaby(true)
	zombie.TicksFrozen = rules.FullyFrozenTicks
	zombie.underfootAir = false

	base := kinds[TypeZombie].speed
	want := (base + rules.FreezeSpeedAmount(rules.FullyFrozenTicks)) * 1.5
	if got := zombie.pace(); !closeEnough(got, want) {
		t.Errorf("a frozen baby zombie runs at %v, want the %v of the slow before the haste", got, want)
	}
}

// TestAMagmaCubeFreezesHarder checks the kind the freeze hurts extra: a
// magma cube's frozen bite takes five half-hearts where a cow's takes one.
// vanilla: freeze_hurts_extra_types
func TestAMagmaCubeFreezesHarder(t *testing.T) {
	w := snowPit(t)
	cube := aimless(NewMob(TypeMagmaCube, 1, 0.5, 64, 0.5, 1))
	cube.NoAI = true
	// A size with the health to take a bite of five and stay standing.
	cube.SetSize(3, true)

	// The counter's first bite past the full comes on the tick it reaches
	// the hundred and sixtieth, twenty in.
	for range rules.FullyFrozenTicks + rules.FreezeHurtTicks/2 - 1 {
		cube.Tick(w, nil)
	}
	events := cube.Tick(w, nil)
	var bite *MobEvent
	for i := range events {
		if events[i].Kind == MobBurned {
			bite = &events[i]
		}
	}
	if bite == nil || bite.DamageType != rules.DamageTypeFreeze {
		t.Fatalf("the magma cube's frozen tick came to %v, want the freeze's bite alone", events)
	}
	if want := cube.MaxHealth - 5; !closeEnough(cube.Health, want) {
		t.Errorf("the magma cube's frozen bite left %v health, want the %v of five off", cube.Health, want)
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

// TestBrokenFireProtectionLeavesAMobsBurnWhole checks that a broken piece's
// burning_time modifier stays off: 160 ticks stay 160.
// vanilla: ItemStack.forEachModifier
func TestBrokenFireProtectionLeavesAMobsBurnWhole(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	id, _ := registry.ItemID("minecraft:diamond_boots")
	ench, _ := registry.EnchantmentID("minecraft:fire_protection")
	cow.Worn[0] = protocol.ItemStack{ItemID: id, Count: 1, Damage: registry.MaxDamage(id), Enchantments: map[int32]int32{ench: 4}}
	cow.IgniteForTicks(160)
	if cow.FireTicks != 160 {
		t.Errorf("the cow in broken boots burns for %d ticks, want 160", cow.FireTicks)
	}
}
