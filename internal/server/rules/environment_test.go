package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The environment's own arithmetic, pinned where the wire cannot reach: the
// burn counters, the breath, and the gates the blocks sit behind.

// dice is a fixed random source, so the fire ignition's one-or-two ticks
// lands the same way every run.
func dice() *rand.Rand { return rand.New(rand.NewSource(7)) }

// stateOf is the default state ID of the named block, for the tests that
// stand a block in a habitat. Blocks that carry no item form — water, lava,
// fire — are not in the placeable table, so the lookup falls to every state
// the block has, the default first; one that is in neither table, the
// propertyless soul fire among them, is found by walking the state IDs,
// which is what a test can afford and a server cannot.
func stateOf(t *testing.T, name string) uint16 {
	t.Helper()
	if state, ok := registry.BlockStates[name]; ok {
		return state
	}
	if states, ok := registry.AllStates[name]; ok && len(states) > 0 {
		return states[0].ID
	}
	for id := range 30000 {
		if found, _, ok := registry.BlockStateName(uint16(id)); ok && found == name {
			return uint16(id)
		}
	}
	t.Fatalf("no block state for %s", name)
	return 0
}

// stateWith is the named block's default state with props set through
// registry.WithProps; a property or value the block lacks fails the test
// instead of falling back to the default state the way FindState does.
func stateWith(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	b, ok := registry.BlockByName(name)
	if !ok {
		t.Fatalf("no block %s", name)
	}
	values := make([]registry.PropValue, 0, len(props))
	for k, v := range props {
		p, ok := registry.PropNamed(k)
		if !ok {
			t.Fatalf("no block has the property %s", k)
		}
		values = append(values, registry.PropValue{P: p, V: v})
	}
	state, ok := registry.WithProps(b.DefaultState(), values...)
	if !ok {
		t.Fatalf("%s has no state with %v", name, props)
	}
	return state
}

// TestLavaIgnitesAndBites checks the lava's work on the counters: the burn
// it leaves is fifteen seconds' worth, the burn tick does not bite a body
// that stands in the lava, and the lava's own four lands every tick the
// body stands there.
func TestLavaIgnitesAndBites(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	lava := stateOf(t, "minecraft:lava")
	around := Habitat{Overlapped: []uint16{lava}}

	hurts, _ := TickEnvironment(&v, around, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeLava || hurts[0].Amount != 4 {
		t.Fatalf("the first tick in lava came to %v, want one blow of four", hurts)
	}
	if v.FireTicks != LavaIgniteTicks {
		t.Errorf("the lava left %d ticks of burn, want %d", v.FireTicks, LavaIgniteTicks)
	}

	// The next ticks keep the burn topped up and bite again only when the
	// invulnerability window the caller runs lets them; the counters'
	// share is that the burn stands at fifteen seconds and the on-fire
	// tick stays quiet in the lava.
	for range 25 {
		hurts, _ = TickEnvironment(&v, around, dice(), 0)
		for _, h := range hurts {
			if h.DamageType != DamageTypeLava {
				t.Errorf("a tick in lava bit with %s, want the lava's own alone", h.DamageType)
			}
		}
		if v.FireTicks != LavaIgniteTicks && v.FireTicks != LavaIgniteTicks-1 {
			t.Errorf("a tick in lava left the burn at %d, want it topped up or a tick spent", v.FireTicks)
		}
	}
}

// TestBurnBitesOnceASecond checks the on-fire tick once the body is out of
// the lava: the burn spends itself a tick at a time, bites one half-heart
// on every tick the counter sits a whole second into, and stops biting when
// it has burnt out.
func TestBurnBitesOnceASecond(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = LavaIgniteTicks

	bites := 0
	for tick := 1; tick <= LavaIgniteTicks; tick++ {
		hurts, _ := TickEnvironment(&v, Habitat{}, dice(), 0)
		for _, h := range hurts {
			if h.DamageType == DamageTypeOnFire {
				// The bite was offered on the tick the counter sat a
				// whole second into, before the tick spent one of it.
				if (v.FireTicks+1)%FireSecondTicks != 0 {
					t.Errorf("tick %d bit with the counter at %d, want the bites only on the seconds", tick, v.FireTicks+1)
				}
				bites++
			} else {
				t.Errorf("tick %d bit with %s in the open air, want the burn's own alone", tick, h.DamageType)
			}
		}
	}
	// Fifteen seconds of burn, a bite a second, the first on the tick the
	// body left the lava.
	if bites != LavaIgniteTicks/FireSecondTicks {
		t.Errorf("%d on-fire bites out of %d ticks of burn, want one a second", bites, LavaIgniteTicks)
	}
	// And the tick the burn ran out on leaves the body in the douse
	// cooldown, from which a fire block has to walk it back.
	if v.FireTicks != -DouseCooldown {
		t.Errorf("the burnout left the counter at %d, want the cooldown's %d", v.FireTicks, -DouseCooldown)
	}
	hurts, _ := TickEnvironment(&v, Habitat{}, dice(), 0)
	if len(hurts) != 0 {
		t.Errorf("the tick after the burnout bit %v, want nothing", hurts)
	}
}

// TestFireIgnitesAndHolds checks the fire block's work: eight seconds of
// burn the moment a body stands in it, held there while it stands, and the
// fire's own bite every tick the caller's window lets through.
func TestFireIgnitesAndHolds(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	fire := stateOf(t, "minecraft:fire")
	around := Habitat{Overlapped: []uint16{fire}}

	hurts, _ := TickEnvironment(&v, around, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeInFire || hurts[0].Amount != 1 {
		t.Fatalf("the first tick in fire came to %v, want one in_fire blow", hurts)
	}
	if v.FireTicks != FireIgniteTicks {
		t.Errorf("the fire left %d ticks of burn, want %d", v.FireTicks, FireIgniteTicks)
	}

	// A hundred ticks of standing in it: the burn stays at the eight
	// seconds the fire keeps renewing, and the blows are the fire's own
	// and, on the ticks the counter sits a second into, the burn's own
	// beside it — the pair a real tick would let the invulnerability
	// window sort out.
	for range 100 {
		hurts, _ = TickEnvironment(&v, around, dice(), 0)
		for _, h := range hurts {
			if h.DamageType != DamageTypeInFire && h.DamageType != DamageTypeOnFire {
				t.Errorf("a tick in fire bit with %s, want the fire's and the burn's own alone", h.DamageType)
			}
		}
		if v.FireTicks < FireIgniteTicks {
			t.Errorf("a tick in fire let the burn fall to %d, want it held at %d", v.FireTicks, FireIgniteTicks)
		}
	}
}

// TestWaterDousesAndCooldowns checks the water's word: the burn goes out
// the tick the body reaches it, the douse is reported for the fizz, and the
// counter sits in the cooldown a fire block must walk back before it can
// re-light anything.
func TestWaterDousesAndCooldowns(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = LavaIgniteTicks
	water := stateOf(t, "minecraft:water")

	_, douse := TickEnvironment(&v, Habitat{Overlapped: []uint16{water}}, dice(), 0)
	if !douse.Sound {
		t.Error("a tick in water did not report the douse")
	}
	if v.FireTicks != -DouseCooldown {
		t.Errorf("the douse left the counter at %d, want the cooldown's %d", v.FireTicks, -DouseCooldown)
	}

	// Walking into a fire block inside the cooldown: the burn does not
	// take, the fire's bite still lands.
	fire := stateOf(t, "minecraft:fire")
	hurts, _ := TickEnvironment(&v, Habitat{Overlapped: []uint16{water, fire}}, dice(), 0)
	var inFire bool
	for _, h := range hurts {
		if h.DamageType == DamageTypeInFire {
			inFire = true
		}
		if h.DamageType == DamageTypeOnFire {
			t.Error("a doused body burnt of its own accord")
		}
	}
	if !inFire {
		t.Error("the fire block's bite did not land on a doused body")
	}
	if v.FireTicks >= 0 {
		t.Errorf("the fire block lit a doused body: the counter stands at %d", v.FireTicks)
	}

	// And twenty ticks of the fire block's walking later, the burn takes.
	for range DouseCooldown {
		TickEnvironment(&v, Habitat{Overlapped: []uint16{fire}}, dice(), 0)
	}
	if v.FireTicks != FireIgniteTicks {
		t.Errorf("after the cooldown's walk the counter stands at %d, want the fire's %d", v.FireTicks, FireIgniteTicks)
	}
}

// TestLavaReignitesThroughTheCooldown checks the other side of the
// cooldown: the lava's word overrides it outright.
func TestLavaReignitesThroughTheCooldown(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = -DouseCooldown
	lava := stateOf(t, "minecraft:lava")
	TickEnvironment(&v, Habitat{Overlapped: []uint16{lava}}, dice(), 0)
	if v.FireTicks != LavaIgniteTicks {
		t.Errorf("the lava left a doused body at %d ticks of burn, want %d", v.FireTicks, LavaIgniteTicks)
	}
}

// TestRainDouses checks the sky's share of the douse, which vanilla takes
// up after the blocks.
func TestRainDouses(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = FireIgniteTicks
	_, douse := TickEnvironment(&v, Habitat{Raining: true}, dice(), 0)
	if !douse.Sound {
		t.Error("the rain did not report the douse")
	}
	if v.FireTicks != -DouseCooldown {
		t.Errorf("the rain left the counter at %d, want the cooldown's %d", v.FireTicks, -DouseCooldown)
	}
}

// TestCauldronDousesAndCooldowns checks the layered cauldrons' word: a
// burning body inside drops the cauldron a level and sits the fire counter
// twenty ticks below nought, so a fire block has to walk it back before it
// can catch again. The fizz is due only for the burn the cauldron found.
// vanilla: LayeredCauldronBlock.handleEntityOnFireInside, Entity.applyEffectsFromBlocks
func TestCauldronDousesAndCooldowns(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = LavaIgniteTicks
	cell := CauldronCell{X: 0, Y: 64, Z: 0}

	_, douse := TickEnvironment(&v, Habitat{Cauldrons: []CauldronCell{cell}}, dice(), 0)
	if !douse.Sound {
		t.Error("the cauldron did not report the fizz for a body already burning")
	}
	if len(douse.Cauldrons) != 1 || douse.Cauldrons[0] != cell {
		t.Errorf("the cauldron cells = %v, want the cell the body stood in", douse.Cauldrons)
	}
	if v.FireTicks != -DouseCooldown {
		t.Errorf("the cauldron left the counter at %d, want the cooldown's %d", v.FireTicks, -DouseCooldown)
	}

	// The fire block walks the cooldown back a tick at a time: it cannot
	// catch until the twentieth tick has passed.
	fire := stateOf(t, "minecraft:fire")
	for i := range DouseCooldown {
		if v.FireTicks == FireIgniteTicks {
			t.Fatalf("the fire caught on tick %d of the cooldown, want it held until the twentieth", i)
		}
		TickEnvironment(&v, Habitat{Overlapped: []uint16{fire}}, dice(), 0)
	}
	if v.FireTicks < FireIgniteTicks {
		t.Errorf("after the cooldown's walk the counter stands at %d, want the fire's %d", v.FireTicks, FireIgniteTicks)
	}
}

// TestWaterSparesTheFizzForABurnLitThisTick checks that water puts out a
// burn a fire block lit this tick without a fizz, while a body already
// burning still fizzes.
// vanilla: Entity.applyEffectsFromBlocks
func TestWaterSparesTheFizzForABurnLitThisTick(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	fire := stateOf(t, "minecraft:fire")
	water := stateOf(t, "minecraft:water")

	_, douse := TickEnvironment(&v, Habitat{Overlapped: []uint16{fire, water}}, dice(), 0)
	if douse.Sound {
		t.Error("water fizzed for a burn a fire block lit the same tick")
	}
	if v.FireTicks > 0 {
		t.Errorf("water left the body at %d fire ticks, want the flame taken", v.FireTicks)
	}

	v = FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = LavaIgniteTicks
	_, douse = TickEnvironment(&v, Habitat{Overlapped: []uint16{fire, water}}, dice(), 0)
	if !douse.Sound {
		t.Error("water did not fizz for a body that was burning before the tick")
	}
}

// TestRainSparesTheFizzForABurnLitThisTick checks that rain puts out a burn a
// fire block lit this tick without a fizz, while a body already burning
// still fizzes.
// vanilla: Entity.applyEffectsFromBlocks
func TestRainSparesTheFizzForABurnLitThisTick(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	fire := stateOf(t, "minecraft:fire")

	_, douse := TickEnvironment(&v, Habitat{Overlapped: []uint16{fire}, Raining: true}, dice(), 0)
	if douse.Sound {
		t.Error("rain fizzed for a burn a fire block lit the same tick")
	}
	if v.FireTicks > 0 {
		t.Errorf("rain left the body at %d fire ticks, want the flame taken", v.FireTicks)
	}

	v = FreshVitals(protocol.GameModeSurvival)
	v.FireTicks = LavaIgniteTicks
	_, douse = TickEnvironment(&v, Habitat{Overlapped: []uint16{fire}, Raining: true}, dice(), 0)
	if !douse.Sound {
		t.Error("rain did not fizz for a body that was burning before the tick")
	}
}

// TestCauldronSparesTheFizzForABurnLitThisTick checks the sound's own gate: a
// fire block lighting the body the same tick the cauldron takes the burn back
// leaves no fizz, since vanilla plays it only for a burn the blocks found.
// The cauldron still takes the flame and drops the level.
// vanilla: Entity.applyEffectsFromBlocks
func TestCauldronSparesTheFizzForABurnLitThisTick(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	fire := stateOf(t, "minecraft:fire")
	cell := CauldronCell{X: 0, Y: 64, Z: 0}

	_, douse := TickEnvironment(&v, Habitat{Overlapped: []uint16{fire}, Cauldrons: []CauldronCell{cell}}, dice(), 0)
	if douse.Sound {
		t.Error("the cauldron fizzed for a burn a fire block lit the same tick")
	}
	if len(douse.Cauldrons) != 1 {
		t.Errorf("the cauldron cells = %v, want the cell the flame took", douse.Cauldrons)
	}
	if v.FireTicks > 0 {
		t.Errorf("the cauldron left the body at %d fire ticks, want the flame taken", v.FireTicks)
	}
}

// TestCauldronSparesTheSpectator checks the block-effects gate: a spectator
// has no block effects, so a cauldron leaves their burn and its fill alone.
// vanilla: LayeredCauldronBlock.handleEntityOnFireInside, Entity.isAffectedByBlocks
func TestCauldronSparesTheSpectator(t *testing.T) {
	v := FreshVitals(protocol.GameModeSpectator)
	v.FireTicks = 5
	cell := CauldronCell{X: 0, Y: 64, Z: 0}

	_, douse := TickEnvironment(&v, Habitat{Cauldrons: []CauldronCell{cell}}, dice(), 0)
	if len(douse.Cauldrons) != 0 {
		t.Errorf("the cauldron took a spectator's burn: cells = %v", douse.Cauldrons)
	}
	if douse.Sound {
		t.Error("the cauldron fizzed for a spectator")
	}
}

// TestLayeredCauldronPredicate checks which cauldrons carry a fill a burning
// body drops: the water and powder-snow ones, not the empty or lava ones.
// vanilla: LayeredCauldronBlock
func TestLayeredCauldronPredicate(t *testing.T) {
	for _, name := range []string{"minecraft:water_cauldron", "minecraft:powder_snow_cauldron"} {
		if !IsLayeredCauldron(stateOf(t, name)) {
			t.Errorf("%s carries no fill, want it to", name)
		}
	}
	for _, name := range []string{"minecraft:cauldron", "minecraft:lava_cauldron"} {
		if IsLayeredCauldron(stateOf(t, name)) {
			t.Errorf("%s carries a fill, want it left out", name)
		}
	}
}

// TestBreathRunsDownAndBack checks the air: a tick a tick under water, the
// bite at twenty past nothing with the counter reset for the next, and four
// a tick back up out of the water to the full breath.
func TestBreathRunsDownAndBack(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)

	// Three hundred and nineteen ticks under water: the breath has run one
	// short of the floor, and nothing has bitten.
	for i := range MaxAirSupply - AirDrownFloor - 1 {
		hurts, _ := TickEnvironment(&v, Habitat{EyesInWater: true}, dice(), 0)
		if len(hurts) != 0 {
			t.Fatalf("tick %d under water bit %v, want the bite to wait for the floor", i, hurts)
		}
	}
	if want := -19; v.AirSupply != want {
		t.Fatalf("the breath stands at %d, want %d", v.AirSupply, want)
	}
	// The three hundred and twentieth drowns two, and the counter resets
	// for the body still under water.
	hurts, _ := TickEnvironment(&v, Habitat{EyesInWater: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeDrown || hurts[0].Amount != 2 {
		t.Fatalf("the floor tick came to %v, want one drown blow of two", hurts)
	}
	if v.AirSupply != 0 {
		t.Errorf("the drowning left the breath at %d, want it reset", v.AirSupply)
	}
	// The next bite is the floor's twenty ticks behind.
	for i := range -AirDrownFloor - 1 {
		hurts, _ = TickEnvironment(&v, Habitat{EyesInWater: true}, dice(), 0)
		if len(hurts) != 0 {
			t.Fatalf("tick %d after the drowning bit %v, want the next at the twentieth", i, hurts)
		}
	}
	hurts, _ = TickEnvironment(&v, Habitat{EyesInWater: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeDrown {
		t.Fatalf("the twentieth tick after the drowning came to %v, want the next bite", hurts)
	}

	// Out of the water the air comes back four a tick to the full breath.
	for range MaxAirSupply / airRefillPerTick {
		TickEnvironment(&v, Habitat{}, dice(), 0)
	}
	if v.AirSupply != MaxAirSupply {
		t.Errorf("the breath climbed back to %d, want the full %d", v.AirSupply, MaxAirSupply)
	}
}

// TestSuffocationAndPricksGated checks the gates the one-point blows sit
// behind: the wall bites only where the eyes are, the berry bush only where
// it bears and the body moves, the magma only under a standing body that is
// not sneaking.
func TestSuffocationAndPricksGated(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	hurts, _ := TickEnvironment(&v, Habitat{EyesInWall: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeInWall {
		t.Fatalf("a tick in the wall came to %v, want one in_wall blow", hurts)
	}

	bush := stateOf(t, "minecraft:sweet_berry_bush")
	for _, state := range registry.AllStates["minecraft:sweet_berry_bush"] {
		if state.Props == "age=3" {
			bush = state.ID
		}
	}
	// A standing body in a fruiting bush: nothing.
	hurts, _ = TickEnvironment(&v, Habitat{Overlapped: []uint16{bush}}, dice(), 0)
	if len(hurts) != 0 {
		t.Errorf("a standing body was pricked %v, want the bush to wait", hurts)
	}
	// A moving body in a growing bush: nothing.
	for _, state := range registry.AllStates["minecraft:sweet_berry_bush"] {
		if state.Props == "age=0" {
			hurts, _ = TickEnvironment(&v, Habitat{Overlapped: []uint16{state.ID}, Moving: true}, dice(), 0)
			if len(hurts) != 0 {
				t.Errorf("a growing bush pricked %v", hurts)
			}
		}
	}
	// A moving body in a fruiting bush: the prick.
	hurts, _ = TickEnvironment(&v, Habitat{Overlapped: []uint16{bush}, Moving: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeSweetBerryBush {
		t.Errorf("a step through the bush came to %v, want one prick", hurts)
	}
	// A sneaking body is pricked all the same.
	hurts, _ = TickEnvironment(&v, Habitat{Overlapped: []uint16{bush}, Moving: true, Sneaking: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeSweetBerryBush {
		t.Errorf("a sneaking step through the bush came to %v, want one prick", hurts)
	}

	magma := stateOf(t, "minecraft:magma_block")
	hurts, _ = TickEnvironment(&v, Habitat{Ground: magma, OnGround: true}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeHotFloor {
		t.Errorf("standing on magma came to %v, want one sear", hurts)
	}
	hurts, _ = TickEnvironment(&v, Habitat{Ground: magma, OnGround: true, Sneaking: true}, dice(), 0)
	if len(hurts) != 0 {
		t.Errorf("a sneaking body was seared %v, want the magma to spare them", hurts)
	}
	hurts, _ = TickEnvironment(&v, Habitat{Ground: magma}, dice(), 0)
	if len(hurts) != 0 {
		t.Errorf("a body over magma but not on it was seared %v", hurts)
	}

	cactus := stateOf(t, "minecraft:cactus")
	hurts, _ = TickEnvironment(&v, Habitat{Overlapped: []uint16{cactus}}, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeCactus {
		t.Errorf("standing against the cactus came to %v, want one prick", hurts)
	}
}

// TestSuffocatingBlocks checks the predicate's answers where the wire would
// meet them: the full-cube solids crush, and so do the four partial blocks
// vanilla registers with isSuffocating(Blocks::always), while the blocks
// vanilla exempts by name — the glasses, the leaves, the grates, the roots
// — do not crush however full their shape.
func TestSuffocatingBlocks(t *testing.T) {
	for _, name := range []string{
		"minecraft:stone", "minecraft:dirt", "minecraft:bedrock", "minecraft:barrier",
		"minecraft:soul_sand", "minecraft:mud", "minecraft:farmland", "minecraft:dirt_path",
	} {
		if !IsSuffocating(stateOf(t, name)) {
			t.Errorf("%s does not crush, want it to", name)
		}
	}
	for _, name := range []string{
		"minecraft:air", "minecraft:water", "minecraft:lava", "minecraft:oak_slab", "minecraft:oak_fence",
		"minecraft:torch", "minecraft:cobweb", "minecraft:short_grass",
		"minecraft:glass", "minecraft:tinted_glass", "minecraft:white_stained_glass",
		"minecraft:oak_leaves", "minecraft:azalea_leaves", "minecraft:mangrove_roots",
		"minecraft:copper_grate", "minecraft:waxed_oxidized_copper_grate",
		// A full cube that is not solid: vanilla's isSuffocating needs
		// blocksMotion, so it never crushes.
		"minecraft:chorus_flower",
	} {
		if IsSuffocating(stateOf(t, name)) {
			t.Errorf("%s crushes, want it not to", name)
		}
	}
	// A retracted piston's full cube crushes where the extended one's
	// 4/16 plate does not.
	// vanilla: Blocks' NOT_EXTENDED_PISTON
	for extended, want := range map[string]bool{"false": true, "true": false} {
		state, ok := registry.FindState("minecraft:piston", map[string]string{
			"extended": extended, "facing": "north"})
		if !ok {
			t.Fatalf("the registry has no piston[extended=%s]", extended)
		}
		if got := IsSuffocating(state); got != want {
			t.Errorf("piston[extended=%s] crushes = %v, want %v", extended, got, want)
		}
	}
}

// stubBlocks stands the cells the eye walk reads, a BlockAt for tests.
type stubBlocks map[[3]int32]uint16

func (w stubBlocks) BlockAt(x, y, z int32) uint16 { return w[[3]int32{x, y, z}] }

// TestEyesInWallReadsTheEyeSlab checks the walk's reach against the body:
// a suffocating block at the cells the eye slab alone crosses bites, the
// cells the feet or the crown fill alone do not, a block that never
// suffocates — glass — bites nowhere, and the partial
// isSuffocating(Blocks::always) blocks bite only where their collision
// still reaches the slab.
func TestEyesInWallReadsTheEyeSlab(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	glass := stateOf(t, "minecraft:glass")
	soulsand := stateOf(t, "minecraft:soul_sand")
	farmland := stateOf(t, "minecraft:farmland")
	for _, tc := range []struct {
		name      string
		cells     stubBlocks
		x, y, z   float64
		width     float64
		eyeHeight float64
		want      bool
	}{
		{"stone at the eyes", stubBlocks{{0, 65, 0}: stone}, 0.5, 64, 0.5, 0.6, 1.6, true},
		{"stone at the feet alone", stubBlocks{{0, 64, 0}: stone}, 0.5, 64, 0.5, 0.6, 1.6, false},
		{"stone at the cell above the eyes", stubBlocks{{0, 66, 0}: stone}, 0.5, 64, 0.5, 0.6, 1.6, false},
		{"stone over the slab's neighbouring cell", stubBlocks{{-1, 65, 0}: stone}, 0.0, 64, 0.5, 0.6, 1.6, true},
		{"glass at the eyes", stubBlocks{{0, 65, 0}: glass}, 0.5, 64, 0.5, 0.6, 1.6, false},
		// The eye heights stand the eye 0.5, 0.95 and 0.9 into the block's
		// own cell: 0.5 sits below both partial tops, 0.95 above both
		// (14/16 and 15/16), and 0.9 below farmland's 15/16 alone.
		{"soul sand at the eye 0.5 into the cell", stubBlocks{{0, 65, 0}: soulsand}, 0.5, 64, 0.5, 0.6, 1.5, true},
		{"soul sand at the eye 0.95 into the cell", stubBlocks{{0, 65, 0}: soulsand}, 0.5, 64, 0.5, 0.6, 1.95, false},
		{"farmland at the eye 0.9 into the cell", stubBlocks{{0, 65, 0}: farmland}, 0.5, 64, 0.5, 0.6, 1.9, true},
	} {
		if got := EyesInWall(tc.cells, tc.x, tc.y, tc.z, tc.width, tc.eyeHeight); got != tc.want {
			t.Errorf("%s: EyesInWall = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestOnPosReadsTheSupportingBlock checks the landing block lookup of
// vanilla's getOnPosLegacy: the cell a fifth of a block below the feet wins
// when it carries collision, a body whose centre cell has none reads the
// block with collision its box stands on, and a block with no collision
// beside the edge — a flower — supports nothing.
func TestOnPosReadsTheSupportingBlock(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	hay := stateOf(t, "minecraft:hay_block")
	flower := stateOf(t, "minecraft:poppy")
	for _, tc := range []struct {
		name string
		w    stubBlocks
		x, z float64
		want uint16
	}{
		{"the cell below the feet", stubBlocks{{0, 63, 0}: stone}, 0.5, 0.5, stone},
		{"the hay under the feet", stubBlocks{{0, 63, 0}: hay}, 0.5, 0.5, hay},
		{"the hay under the edge", stubBlocks{{0, 63, 0}: hay}, 1.2, 0.5, hay},
		{"a flower under the centre gives way to the hay beside", stubBlocks{{0, 63, 0}: hay, {1, 63, 0}: flower}, 1.2, 0.5, hay},
		{"a flower under the edge supports nothing", stubBlocks{{0, 63, 0}: flower}, 1.2, 0.5, airState},
		{"air under the feet", stubBlocks{{0, 64, 0}: stone}, 0.5, 0.5, airState},
	} {
		if got := OnPos(tc.w, tc.x, 64, tc.z, 0.3); got != tc.want {
			t.Errorf("%s: OnPos = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestOnPosReadsTheFencePost checks that a body standing on a fence, wall or
// fence gate reads that block, whose collision reaches 1.5 up.
func TestOnPosReadsTheFencePost(t *testing.T) {
	for _, name := range []string{"minecraft:oak_fence", "minecraft:cobblestone_wall", "minecraft:oak_fence_gate"} {
		post := stateOf(t, name)
		w := stubBlocks{{0, 64, 0}: post}
		if got := OnPos(w, 0.5, 65.5, 0.5, 0.3); got != post {
			t.Errorf("%s: OnPos = %d, want %d", name, got, post)
		}
	}
	if got := OnPos(stubBlocks{{0, 64, 0}: stateOf(t, "minecraft:stone")}, 0.5, 65.5, 0.5, 0.3); got != airState {
		t.Errorf("stone a block too low: OnPos = %d, want air", got)
	}
}

// TestFireDamageExhaustion checks what the environment's blows cost the
// food bar, the damage types' own files' exhaustion field: the burns and
// pricks a tenth, the burning tick and the drowning and the crush nothing.
func TestFireDamageExhaustion(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	Exhaust(&v, DamageExhaustion(DamageTypeLava))
	if v.Exhaustion != 0.1 {
		t.Errorf("a lava bite cost %v exhaustion, want 0.1", v.Exhaustion)
	}
	v = FreshVitals(protocol.GameModeSurvival)
	Exhaust(&v, DamageExhaustion(DamageTypeOnFire))
	if v.Exhaustion != 0 {
		t.Errorf("the on-fire tick cost %v exhaustion, want none", v.Exhaustion)
	}
}

// TestEnvironmentSparesTheUntouchable checks the rules' own gate for a mode
// that cannot be hurt: the blocks light no new flame and deal no blow, but
// the burn the body already carries still counts down, held at one by
// Player's clamp.
func TestEnvironmentSparesTheUntouchable(t *testing.T) {
	creative := FreshVitals(protocol.GameModeCreative)
	lava := stateOf(t, "minecraft:lava")
	hurts, _ := TickEnvironment(&creative, Habitat{Overlapped: []uint16{lava}}, dice(), 0)
	if len(hurts) != 0 {
		t.Errorf("the rules hurt an invulnerable body: %v", hurts)
	}
	if creative.FireTicks != -DouseCooldown {
		t.Errorf("the invulnerable body in lava sat at %d fire ticks, want the douse cooldown", creative.FireTicks)
	}

	burning := FreshVitals(protocol.GameModeCreative)
	burning.FireTicks = 100
	TickEnvironment(&burning, Habitat{}, dice(), 0)
	if burning.FireTicks != 1 {
		t.Errorf("the invulnerable body's burn = %d, want Player's clamp at one", burning.FireTicks)
	}
}

// TestFireThawsOnlyWhenItCanLight checks the flame's thaw against the douse
// cooldown: a fire block only ignites a counter at nought or above, so it
// thaws only then, and a body still counting the cooldown keeps its freeze.
// vanilla: BaseFireBlock.fireIgnite, Entity.igniteForTicks
func TestFireThawsOnlyWhenItCanLight(t *testing.T) {
	fire := stateOf(t, "minecraft:fire")

	cooled := FreshVitals(protocol.GameModeSurvival)
	cooled.FireTicks = -DouseCooldown
	cooled.TicksFrozen = FullyFrozenTicks
	TickEnvironment(&cooled, Habitat{Overlapped: []uint16{fire}}, nil, 0)
	if cooled.TicksFrozen != FullyFrozenTicks {
		t.Errorf("a fire that could not light a doused body thawed it to %d, want the %d held", cooled.TicksFrozen, FullyFrozenTicks)
	}

	lit := FreshVitals(protocol.GameModeSurvival)
	lit.TicksFrozen = FullyFrozenTicks
	TickEnvironment(&lit, Habitat{Overlapped: []uint16{fire}}, nil, 0)
	if lit.TicksFrozen != 0 {
		t.Errorf("a lit body kept %d frozen ticks, want the flame to thaw it", lit.TicksFrozen)
	}
}

// TestStandingBlockFollowsTheFeetBox checks the cell the mace's dust rises
// from: the cell under the feet box, not only under the centre column, and a
// fence's post below an air cell.
func TestStandingBlockFollowsTheFeetBox(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	fence := stateOf(t, "minecraft:oak_fence")
	for _, tc := range []struct {
		name       string
		w          stubBlocks
		x, y       float64
		bx, by, bz int32
	}{
		{"under the centre", stubBlocks{{0, 63, 0}: stone}, 0.5, 64, 0, 63, 0},
		{"under the edge of the box", stubBlocks{{5, 63, 0}: stone}, 4.9, 64, 5, 63, 0},
		{"a fence post below the air", stubBlocks{{0, 63, 0}: fence}, 0.5, 64.5, 0, 63, 0},
		{"nothing below", stubBlocks{}, 0.5, 64, 0, 63, 0},
	} {
		bx, by, bz := StandingBlock(tc.w, tc.x, tc.y, 0.5, 0.3)
		if bx != tc.bx || by != tc.by || bz != tc.bz {
			t.Errorf("%s: StandingBlock = (%d, %d, %d), want (%d, %d, %d)",
				tc.name, bx, by, bz, tc.bx, tc.by, tc.bz)
		}
	}
}

// TestBurningTimeScalesFireAndLavaIgnition checks that a fire block and lava
// light a body for the burning_time share of their ticks.
// vanilla: LivingEntity.igniteForTicks
func TestBurningTimeScalesFireAndLavaIgnition(t *testing.T) {
	fire := stateOf(t, "minecraft:fire")
	lava := stateOf(t, "minecraft:lava")
	for _, tc := range []struct {
		name  string
		state uint16
		want  int
	}{{"fire", fire, 64}, {"lava", lava, 120}} {
		v := FreshVitals(protocol.GameModeSurvival)
		TickEnvironment(&v, Habitat{Overlapped: []uint16{tc.state}, BurningTimeDelta: -0.6}, nil, 0)
		if v.FireTicks != tc.want {
			t.Errorf("%s with burning_time 0.4 lit %d ticks, want %d", tc.name, v.FireTicks, tc.want)
		}
	}
}

// TestLavaCauldronIgnitesAndBites checks that a body in a lava cauldron
// takes lava's burn and four, and keeps the burn tick's own bite that only
// the fluid silences.
// vanilla: LavaCauldronBlock.entityInside
func TestLavaCauldronIgnitesAndBites(t *testing.T) {
	v := FreshVitals(protocol.GameModeSurvival)
	around := Habitat{Overlapped: []uint16{stateOf(t, "minecraft:lava_cauldron")}}

	hurts, _ := TickEnvironment(&v, around, dice(), 0)
	if len(hurts) != 1 || hurts[0].DamageType != DamageTypeLava || hurts[0].Amount != 4 {
		t.Fatalf("a tick in a lava cauldron came to %v, want one blow of four", hurts)
	}
	if v.FireTicks != LavaIgniteTicks {
		t.Errorf("the cauldron left %d ticks of burn, want %d", v.FireTicks, LavaIgniteTicks)
	}
	spectator := FreshVitals(protocol.GameModeSpectator)
	if hurts, _ := TickEnvironment(&spectator, around, dice(), 0); len(hurts) != 0 || spectator.FireTicks > 0 {
		t.Errorf("a spectator took %v and %d burn from the cauldron, want none", hurts, spectator.FireTicks)
	}
}
