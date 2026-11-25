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
func stateOf(t testing.TB, name string) uint16 {
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

// stubBlocks stands the cells the eye walk reads, a BlockAt for tests.
type stubBlocks map[[3]int32]uint16

func (w stubBlocks) BlockAt(x, y, z int32) uint16 { return w[[3]int32{x, y, z}] }

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

// countingBlocks counts the BlockAt reads made through it.
type countingBlocks struct {
	stubBlocks
	reads int
}

func (w *countingBlocks) BlockAt(x, y, z int32) uint16 {
	w.reads++
	return w.stubBlocks.BlockAt(x, y, z)
}

var standingFixtures = func(t testing.TB) []struct {
	name     string
	w        stubBlocks
	x, y, z  float64
	maxReads int
} {
	stone := stateOf(t, "minecraft:stone")
	fence := stateOf(t, "minecraft:oak_fence")
	return []struct {
		name     string
		w        stubBlocks
		x, y, z  float64
		maxReads int
	}{
		{"centred stone", stubBlocks{{0, 63, 0}: stone}, 0.5, 64, 0.5, 1},
		{"stone edge", stubBlocks{{0, 63, 0}: stone}, 1.1, 64, 0.5, 4},
		{"fence", stubBlocks{{0, 63, 0}: fence}, 0.5, 64.5, 0.5, 2},
		{"corner centre hit", stubBlocks{{0, 63, 0}: stone, {1, 63, 1}: stone}, 1.05, 64, 1.05, 1},
		{"corner centre miss", stubBlocks{{1, 63, 1}: stone}, 0.95, 64, 0.95, 8},
	}
}

// TestOnPosCellReadsEachCellOnce checks that one ground-contact lookup
// carries the state it read instead of rereading the cell.
func TestOnPosCellReadsEachCellOnce(t *testing.T) {
	for _, tc := range standingFixtures(t) {
		w := &countingBlocks{stubBlocks: tc.w}
		bx, by, bz := StandingBlock(tc.w, tc.x, tc.y, tc.z, 0.125)
		ox, oy, oz, state := onPosCell(w, tc.x, tc.y, tc.z, 0.125)
		if ox != bx || oy != by || oz != bz || state != tc.w[[3]int32{bx, by, bz}] {
			t.Errorf("%s: got (%d,%d,%d) state %d, want (%d,%d,%d)", tc.name, ox, oy, oz, state, bx, by, bz)
		}
		if w.reads > tc.maxReads {
			t.Errorf("%s: %d reads, want at most %d", tc.name, w.reads, tc.maxReads)
		}
	}
}

func BenchmarkOnPosCell(b *testing.B) {
	for _, tc := range standingFixtures(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				onPosCell(tc.w, tc.x, tc.y, tc.z, 0.125)
			}
		})
	}
}

// TestSupportCellPrefersTheNearestThenTheGreater checks the support choice:
// a shape stopping below the feet is no support, the nearest crossing cell
// wins, the centre column among them, and equal distances go to the greater
// Z, then X.
func TestSupportCellPrefersTheNearestThenTheGreater(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	path := stateOf(t, "minecraft:dirt_path")
	fence := stateOf(t, "minecraft:oak_fence")
	slab := stateOf(t, "minecraft:stone_slab")
	for _, tc := range []struct {
		name         string
		w            stubBlocks
		x, y, z      float64
		wantX, wantZ int32
	}{
		{"short block gives way to stone", stubBlocks{{0, 63, 0}: path, {0, 63, 1}: stone}, 0.5, 64, 0.875, 0, 1},
		{"equal Z goes to the greater", stubBlocks{{0, 63, 0}: path, {0, 63, 1}: path}, 0.5, 63.9375, 1, 0, 1},
		{"equal X goes to the greater", stubBlocks{{0, 63, 0}: path, {1, 63, 0}: path}, 1, 63.9375, 0.5, 1, 0},
		{"centre column beats a corner", stubBlocks{{0, 63, 0}: stone, {1, 63, 1}: stone}, 0.95, 64, 0.95, 0, 0},
		{"centre column wins an exact boundary", stubBlocks{{0, 63, 0}: stone, {1, 63, 0}: stone}, 1, 64, 0.5, 1, 0},
		{"the lower row's nearer fence beats the upper row's slab", stubBlocks{{0, 63, 0}: fence, {1, 64, 1}: slab}, 0.71, 64.5, 0.71, 0, 0},
	} {
		bx, _, bz, _, found := supportCell(tc.w, tc.x, tc.y, tc.z, 0.3)
		if !found || bx != tc.wantX || bz != tc.wantZ {
			t.Errorf("%s: got (%d,%d) found %v, want (%d,%d)", tc.name, bx, bz, found, tc.wantX, tc.wantZ)
		}
	}
}

func TestOnPosCellMovedRetriesAtTheBoxBeforeTheStep(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	w := stubBlocks{{0, 63, 0}: stone}
	// Box 1.1..1.7 clears the stone; before a +0.5 step it spanned 0.6..1.2.
	if _, _, _, s := OnPosCell(w, 1.4, 64, 0.5, 0.3); s == stone {
		t.Fatal("without a movement nothing supports the body")
	}
	x, y, z, s := OnPosCellMoved(w, 1.4, 64, 0.5, 0.3, 0.5, 0)
	if x != 0 || y != 63 || z != 0 || s != stone {
		t.Errorf("got (%d,%d,%d) state %d, want the stone at (0,63,0)", x, y, z, s)
	}
}
