package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The falling block simulation as ordinary unit tests, driving the step
// over a world that is nothing but the blocks each case sets — the same
// plainest world the item's tests step against. No server is involved; the
// black-box tests in internal/server/*_test.go remain the proof of the whole life of
// a fall on the wire, and these are the cheapest possible checks of the
// tick's own decisions: the landing's place test, the powder's
// solidification, the drop a refused landing gives, and the timeouts.

// fallTick steps a fresh falling body from rest at the position given,
// carrying the state given, in the world given, until its tick ends it —
// a landing takes a few ticks of fall before the body comes to rest.
func fallTick(t *testing.T, w World, x, y, z float64, state uint16) (*FallingBlock, []FallingEvent) {
	t.Helper()
	f := NewFallingBlock(1, x, y, z, 0, 0, 0, state, 1)
	var events []FallingEvent
	for i := 0; !f.Dead && i < 1000; i++ {
		events = f.Step(w)
	}
	if !f.Dead {
		t.Fatal("the body never came to rest")
	}
	return f, events
}

// TestAFallIsHeldUpByTheBlockNotItsFluid checks FallingBlock.isFree:
// a block falls through air, the fire tag, a water or lava block, and
// anything the replaceable tag names — but not a water plant or a
// waterlogged solid, whose own cell's water does not free it.
// vanilla: FallingBlock.isFree
func TestAFallIsHeldUpByTheBlockNotItsFluid(t *testing.T) {
	state := func(name string) uint16 { return registry.MustState(name, nil) }
	free := []string{
		"minecraft:air", "minecraft:fire", "minecraft:soul_fire",
		"minecraft:water", "minecraft:lava", "minecraft:short_grass",
		"minecraft:seagrass", "minecraft:tall_seagrass",
	}
	for _, name := range free {
		if !fallingFree(state(name)) {
			t.Errorf("fallingFree(%s) = false, want a cell that falls through", name)
		}
	}
	waterlogged, ok := registry.FindState("minecraft:oak_fence", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("no waterlogged oak fence")
	}
	held := []uint16{
		state("minecraft:kelp"), state("minecraft:kelp_plant"),
		state("minecraft:stone"), waterlogged,
	}
	for _, s := range held {
		name, _, _ := registry.BlockStateName(s)
		if fallingFree(s) {
			t.Errorf("fallingFree(%s) = true, want a cell that holds a falling block", name)
		}
	}
}

// TestALandingPlacesItsBlock checks the plain landing: a body whose feet
// come to rest on a solid floor places the block it carries in the cell its
// feet stand in — the vanilla tick's blockPosition — and ends.
func TestALandingPlacesItsBlock(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]
	stone := registry.BlockStates["minecraft:stone"]
	w.set(4, 63, 2, stone)

	f, events := fallTick(t, &w, 4.5, 64.5, 2.5, sand)
	if !f.Dead {
		t.Fatal("a landed body is not dead")
	}
	if len(events) != 1 || events[0].Kind != FallingLanded {
		t.Fatalf("events = %v, want one landing", events)
	}
	if events[0].X != 4 || events[0].Y != 64 || events[0].Z != 2 {
		t.Errorf("landing at (%d, %d, %d), want (4, 64, 2)", events[0].X, events[0].Y, events[0].Z)
	}
	if events[0].State != sand {
		t.Errorf("landing state = %d, want %d (sand)", events[0].State, sand)
	}
}

// TestALandingOnATorchDrops checks the refusal: a body come to rest in a
// cell a placement may not replace — the torch's, which has no collision to
// land on and so sits under the body's feet — ends in a drop, not a
// placement, the torch cell itself untouched by the decision.
func TestALandingOnATorchDrops(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]
	torch := registry.BlockStates["minecraft:torch"]
	stone := registry.BlockStates["minecraft:stone"]
	w.set(4, 63, 2, stone)
	w.set(4, 64, 2, torch)

	f, events := fallTick(t, &w, 4.5, 64.5, 2.5, sand)
	if !f.Dead {
		t.Fatal("a dropped body is not dead")
	}
	// A landing that cannot place breaks the body before it drops.
	if len(events) != 2 || events[0].Kind != FallingBroken || events[1].Kind != FallingDropped {
		t.Fatalf("events = %v, want a break then a drop", events)
	}
	if b := events[0]; b.X != 4 || b.Y != 64 || b.Z != 2 {
		t.Errorf("break cell = %d %d %d, want the landing cell 4 64 2", b.X, b.Y, b.Z)
	}
	if events[1].State != sand {
		t.Errorf("drop state = %d, want %d (sand)", events[1].State, sand)
	}
	// The drop is at the body's feet, the position vanilla's
	// spawnAtLocation lays the item at.
	if events[1].FeetY != 64.0 {
		t.Errorf("drop at y %g, want the feet's 64.0", events[1].FeetY)
	}
}

// TestAConcretePowderSetsAsConcrete checks the solidification: a powder
// whose landing cell holds water places the concrete of its colour, the
// pair the vanilla block definitions carry, while a powder landing beside
// water — the touchesLiquid half of the vanilla test — does the same.
func TestAConcretePowderSetsAsConcrete(t *testing.T) {
	powder := registry.BlockStates["minecraft:white_concrete_powder"]
	concrete := registry.BlockStates["minecraft:white_concrete"]
	stone := registry.BlockStates["minecraft:stone"]

	t.Run("in the water it lands in", func(t *testing.T) {
		w := newTestWorld()
		w.set(4, 63, 2, stone)
		w.set(4, 64, 2, registry.AllStates["minecraft:water"][0].ID)
		_, events := fallTick(t, &w, 4.5, 65.0, 2.5, powder)
		if len(events) != 1 || events[0].State != concrete {
			t.Fatalf("events = %v, want one landing of white concrete", events)
		}
	})

	t.Run("beside the water it lands next to", func(t *testing.T) {
		w := newTestWorld()
		w.set(4, 63, 2, stone)
		w.set(3, 64, 2, registry.AllStates["minecraft:water"][0].ID)
		_, events := fallTick(t, &w, 4.5, 64.5, 2.5, powder)
		if len(events) != 1 || events[0].State != concrete {
			t.Fatalf("events = %v, want one landing of white concrete", events)
		}
	})

	t.Run("dry it sets as itself", func(t *testing.T) {
		w := newTestWorld()
		w.set(4, 63, 2, stone)
		_, events := fallTick(t, &w, 4.5, 64.5, 2.5, powder)
		if len(events) != 1 || events[0].State != powder {
			t.Fatalf("events = %v, want one landing of the powder itself", events)
		}
	})

	t.Run("on a waterlogged block below", func(t *testing.T) {
		// The block the powder rests on holds water, but no water stands
		// beside the landing: touchesLiquid reads the cell below only under
		// the DOWN a cell that already holds water stands over, so landing
		// on a waterlogged block leaves the powder as it is.
		stair, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "east", "waterlogged": "true"})
		if !ok {
			t.Fatal("no waterlogged oak stair")
		}
		w := newTestWorld()
		w.set(4, 63, 2, stair)
		_, events := fallTick(t, &w, 4.5, 64.5, 2.5, powder)
		if len(events) != 1 || events[0].State != powder {
			t.Fatalf("events = %v, want one landing of the powder itself", events)
		}
	})
}

// TestAFallPastTheWorldBottomIsDropped checks the timeout: a body below the
// world's bottom for the vanilla tick's hundred ticks gives its block up as
// an item, and one without the drop is merely gone — and a body that has
// fallen beneath the dimension's bottom and sixty-four more is gone at once,
// the discard vanilla's base tick runs.
func TestAFallPastTheWorldBottomIsDropped(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]

	// The below-world discard, the first tick beneath the void line.
	f := NewFallingBlock(1, 4.5, float64(w.MinY())-65, 2.5, 0, 0, 0, sand, 1)
	if events := f.Step(&w); len(events) != 0 || !f.Dead {
		t.Fatalf("below-world body: events = %v, dead = %v; want a silent discard", events, f.Dead)
	}

	// A body already past its hundredth tick when it crosses the world's
	// bottom drops the block there — the time rule, which wins over the
	// base tick's discard for a body that entered the void late.
	f = NewFallingBlock(1, 4.5, float64(w.MinY())-1, 2.5, 0, 0, 0, sand, 1)
	f.Time = 100
	var events []FallingEvent
	for i := 0; i < fallingOutOfWorldTicks+2 && !f.Dead; i++ {
		events = f.Step(&w)
	}
	if !f.Dead {
		t.Fatal("a body below the world's bottom never ended")
	}
	if len(events) != 1 || events[0].Kind != FallingDropped {
		t.Fatalf("events = %v, want one drop", events)
	}

	// The drop taken off the body leaves nothing behind.
	f = NewFallingBlock(1, 4.5, float64(w.MinY())-1, 2.5, 0, 0, 0, sand, 1)
	f.DropItem = false
	f.Time = 100
	for i := 0; i < fallingOutOfWorldTicks+2 && !f.Dead; i++ {
		events = f.Step(&w)
	}
	if !f.Dead || len(events) != 0 {
		t.Fatalf("a dropless body's end: events = %v, dead = %v; want a silent discard", events, f.Dead)
	}
}

// TestAFallSixHundredTicksOldIsDropped checks the lifetime: a body still in
// the air after the vanilla tick's six hundred gives its block up wherever
// it stands.
func TestAFallSixHundredTicksOldIsDropped(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]

	// A fall this long needs the head start its terminal pace asks for:
	// six hundred ticks of falling cover a thousand blocks and more.
	f := NewFallingBlock(1, 4.5, 1300, 2.5, 0, 0, 0, sand, 1)
	var events []FallingEvent
	for i := 0; i < fallingLifetimeTicks+1 && !f.Dead; i++ {
		events = f.Step(&w)
	}
	if !f.Dead {
		t.Fatal("a body in the air for six hundred ticks never ended")
	}
	if len(events) != 1 || events[0].Kind != FallingDropped {
		t.Fatalf("events = %v, want one drop", events)
	}
	if f.Time != fallingLifetimeTicks+1 {
		t.Errorf("time = %d, want %d", f.Time, fallingLifetimeTicks+1)
	}
}

// TestTheFallRunsOnVanillasNumbers checks the trajectory against the
// vanilla tick's own arithmetic: gravity subtracted, the move made by it,
// the air drag taken after — so the body's position after N ticks is the
// sum of the moved-by velocities and its stored velocity the dragged one,
// the numbers the packets carry.
func TestTheFallRunsOnVanillasNumbers(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]

	f := NewFallingBlock(1, 4.5, 200, 2.5, 0, 0, 0, sand, 1)
	y := 200.0
	v := 0.0
	for i := range 40 {
		if ev := f.Step(&w); len(ev) != 0 {
			t.Fatalf("tick %d produced events %v over open air", i, ev)
		}
		// The tick's own arithmetic: the velocity the move was made by —
		// the gravity taken off the stored one — and the drag taken off
		// what is stored after the move.
		moved := v - fallingGravity
		y += moved
		v = moved * airDragFloat
		if diff := f.Y - y; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("tick %d: y = %g, want %g", i+1, f.Y, y)
		}
		if diff := f.VY - v; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("tick %d: vy = %g, want %g", i+1, f.VY, v)
		}
	}
}

// rollRand hands back a random source whose first draw is known: a hit
// rolls under every chance the wear rolls against, a miss rolls over the
// largest of them. The seed is found on a throwaway source so the one the
// body rolls is fresh — its first draw is the one the search saw.
func rollRand(hit bool) *rand.Rand {
	for s := int64(1); ; s++ {
		draw := rand.New(rand.NewSource(s)).Float64()
		if (hit && draw < 0.05) || (!hit && draw > 0.5) {
			return rand.New(rand.NewSource(s))
		}
	}
}

// TestAFallTracksItsDistance checks the fall's distance: the downward moves
// of the fall pile up while the body is in the air, and the landing — the
// roll asked before the forgetting — leaves the distance at nothing.
// vanilla: checkFallDamage
func TestAFallTracksItsDistance(t *testing.T) {
	w := newTestWorld()
	sand := registry.BlockStates["minecraft:sand"]
	stone := registry.BlockStates["minecraft:stone"]
	w.set(4, 63, 2, stone)

	f := NewFallingBlock(1, 4.5, 70.0, 2.5, 0, 0, 0, sand, 1)
	for i := 0; !f.Dead && i < 1000; i++ {
		f.Step(&w)
		if !f.OnGround && f.Y < 69 && f.FallDistance == 0 {
			t.Fatalf("tick %d: a fall in the air keeps no distance", i)
		}
	}
	if !f.Dead {
		t.Fatal("the body never came to rest")
	}
	if f.FallDistance != 0 {
		t.Errorf("fall distance at rest = %g, want 0", f.FallDistance)
	}
}

// TestAFallThroughAWebOrWaterLayerForgetsItsDistance holds the fall-reset
// sweep of Entity.move: a long fall through a one-thick layer of web or
// water carries only the steps taken below the layer into the landing.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING
func TestAFallThroughAWebOrWaterLayerForgetsItsDistance(t *testing.T) {
	for _, layer := range []string{"minecraft:cobweb", "minecraft:water"} {
		t.Run(layer, func(t *testing.T) {
			w := newTestWorld()
			stone := registry.BlockStates["minecraft:stone"]
			l := defaultState(t, layer)
			for x := int32(3); x <= 5; x++ {
				for z := int32(1); z <= 3; z++ {
					w.set(x, 40, z, stone)
					w.set(x, 50, z, l)
				}
			}
			f := NewFallingBlock(1, 4.5, 90.0, 2.5, 0, 0, 0, registry.BlockStates["minecraft:anvil"], 1)
			past := false
			worst := 0.0
			for i := 0; !f.Dead && !f.OnGround && i < 1000; i++ {
				f.Step(&w)
				if f.Y < 49 {
					past = true
				}
				if past && f.FallDistance > worst {
					worst = f.FallDistance
				}
			}
			if !past {
				t.Fatal("the body never passed the layer")
			}
			if worst > 12 {
				t.Errorf("fall distance below the layer = %g, want only the short fall", worst)
			}
		})
	}
}

// TestAnAnvilsFallWearsIt checks the wear roll of vanilla's
// causeFallDamage: past the first block of the fall, by the chance that
// distance grows, the anvil steps one state down its chain; short of that
// first block, or with the roll missed, or for a block that is no anvil,
// the fall leaves the block it carries alone.
func TestAnAnvilsFallWearsIt(t *testing.T) {
	stone := registry.BlockStates["minecraft:stone"]

	drive := func(t *testing.T, state uint16, y float64, r *rand.Rand) (*FallingBlock, []FallingEvent) {
		t.Helper()
		w := newTestWorld()
		w.set(4, 63, 2, stone)
		f := NewFallingBlock(1, 4.5, y, 2.5, 0, 0, 0, state, 1)
		f.SetHurtsEntities(2.0, 40)
		f.Rand = r
		var events []FallingEvent
		for i := 0; !f.Dead && i < 1000; i++ {
			events = f.Step(&w)
		}
		if !f.Dead {
			t.Fatal("the body never came to rest")
		}
		// The wear test is about the block the tick lands, not the hurt the
		// same tick reports, which its own test checks.
		var placed []FallingEvent
		for _, ev := range events {
			if ev.Kind != FallingHurt {
				placed = append(placed, ev)
			}
		}
		return f, placed
	}

	t.Run("a hard fall steps it down", func(t *testing.T) {
		anvil := registry.BlockStates["minecraft:anvil"]
		chipped := registry.BlockStates["minecraft:chipped_anvil"]
		_, events := drive(t, anvil, 66.0, rollRand(true))
		if len(events) != 1 || events[0].Kind != FallingLanded {
			t.Fatalf("events = %v, want one landing", events)
		}
		if events[0].State != chipped {
			t.Errorf("landed state = %d, want %d (chipped anvil)", events[0].State, chipped)
		}
	})

	t.Run("a missed roll leaves it", func(t *testing.T) {
		anvil := registry.BlockStates["minecraft:anvil"]
		_, events := drive(t, anvil, 66.0, rollRand(false))
		if len(events) != 1 || events[0].Kind != FallingLanded || events[0].State != anvil {
			t.Fatalf("events = %v, want one landing of the anvil itself", events)
		}
	})

	t.Run("a short fall rolls nothing", func(t *testing.T) {
		// Feet half a block up: the distance never passes the first
		// block of the roll, even with a source that always draws low.
		anvil := registry.BlockStates["minecraft:anvil"]
		_, events := drive(t, anvil, 64.5, rollRand(true))
		if len(events) != 1 || events[0].Kind != FallingLanded || events[0].State != anvil {
			t.Fatalf("events = %v, want one landing of the anvil itself", events)
		}
	})

	t.Run("a worn-out anvil shatters", func(t *testing.T) {
		damaged := registry.BlockStates["minecraft:damaged_anvil"]
		f, events := drive(t, damaged, 70.0, rollRand(true))
		if !f.CancelDrop {
			t.Fatal("a shattered anvil's fall does not cancel its drop")
		}
		if len(events) != 1 || events[0].Kind != FallingBroken || events[0].State != damaged {
			t.Fatalf("events = %v, want only the break — no block placed, no item dropped", events)
		}
	})

	t.Run("a fall that is no anvil's wears nothing", func(t *testing.T) {
		sand := registry.BlockStates["minecraft:sand"]
		_, events := drive(t, sand, 70.0, rollRand(true))
		if len(events) != 1 || events[0].Kind != FallingLanded || events[0].State != sand {
			t.Fatalf("events = %v, want one landing of the sand itself", events)
		}
	})

	t.Run("the facing the wear carries over", func(t *testing.T) {
		eastAnvil, ok := registry.FindState("minecraft:anvil", map[string]string{"facing": "east"})
		if !ok {
			t.Fatal("no anvil state facing east")
		}
		_, events := drive(t, eastAnvil, 66.0, rollRand(true))
		if len(events) != 1 || events[0].Kind != FallingLanded {
			t.Fatalf("events = %v, want one landing", events)
		}
		name, props, _ := registry.BlockStateName(events[0].State)
		if name != "minecraft:chipped_anvil" {
			t.Errorf("landed block = %s, want chipped_anvil", name)
		}
		if got := registry.ParseStateProps(props)["facing"]; got != "east" {
			t.Errorf("landed facing = %q, want east", got)
		}
	})
}

// TestAFallHurtsWhatItLandsOn checks the hurt half of vanilla's
// causeFallDamage: past the first block of the fall, a body that hurts what
// it lands on reports the damage its distance earns — at the per-block
// amount, under the cap — as a FallingHurt event, typed falling_anvil for
// an anvil and falling_block for anything else carrying the numbers; short
// of the first block, or with no hurt carried, nothing is reported.
func TestAFallHurtsWhatItLandsOn(t *testing.T) {
	anvil := registry.BlockStates["minecraft:anvil"]
	chipped := registry.BlockStates["minecraft:chipped_anvil"]

	// A body carrying the anvil's numbers whose wear roll always misses, so
	// the block it lands keeps its state and only the hurt is under test.
	hurting := func(state uint16) *FallingBlock {
		f := NewFallingBlock(1, 4.5, 64, 2.5, 0, 0, 0, state, 1)
		f.SetHurtsEntities(2.0, 40)
		f.Rand = rollRand(false)
		return f
	}

	t.Run("the fall's distance sets the damage", func(t *testing.T) {
		f := hurting(chipped)
		f.FallDistance = 6 // five whole blocks past the first
		events := f.causeFallDamage()
		if len(events) != 1 || events[0].Kind != FallingHurt {
			t.Fatalf("events = %v, want one hurt", events)
		}
		if events[0].Damage != 10 {
			t.Errorf("damage = %g, want 10 (five blocks at two a block)", events[0].Damage)
		}
		if events[0].DamageType != rules.DamageTypeFallingAnvil {
			t.Errorf("type = %q, want %q", events[0].DamageType, rules.DamageTypeFallingAnvil)
		}
	})

	t.Run("the cap holds the damage down", func(t *testing.T) {
		f := hurting(chipped)
		f.FallDistance = 100
		events := f.causeFallDamage()
		if len(events) != 1 || events[0].Damage != 40 {
			t.Fatalf("events = %v, want one hurt of the anvil's forty cap", events)
		}
	})

	t.Run("the first block hurts nothing", func(t *testing.T) {
		f := hurting(anvil)
		f.FallDistance = 1
		if events := f.causeFallDamage(); len(events) != 0 {
			t.Fatalf("events = %v, want no hurt under the first block", events)
		}
	})

	t.Run("a body that carries no hurt reports nothing", func(t *testing.T) {
		f := NewFallingBlock(1, 4.5, 64, 2.5, 0, 0, 0, anvil, 1)
		f.FallDistance = 6
		if events := f.causeFallDamage(); len(events) != 0 {
			t.Fatalf("events = %v, want no hurt from a body that carries none", events)
		}
	})

	t.Run("a plain hurting block is the plain type", func(t *testing.T) {
		f := hurting(registry.BlockStates["minecraft:sand"])
		f.FallDistance = 6
		events := f.causeFallDamage()
		if len(events) != 1 || events[0].DamageType != rules.DamageTypeFallingBlock {
			t.Fatalf("events = %v, want one falling_block hurt", events)
		}
	})

	t.Run("a speleothem's fall is the stalactite type", func(t *testing.T) {
		f := hurting(registry.BlockStates["minecraft:pointed_dripstone"])
		f.FallDistance = 6
		events := f.causeFallDamage()
		if len(events) != 1 || events[0].DamageType != rules.DamageTypeFallingStalactite {
			t.Fatalf("events = %v, want one falling_stalactite hurt", events)
		}
	})
}

// TestAHurtBoxIsTheBodyBox checks the box a landing gathers its victims by:
// the falling body's own box, not one shrunk about it.
// vanilla: FallingBlockEntity.causeFallDamage getBoundingBox
func TestAHurtBoxIsTheBodyBox(t *testing.T) {
	f := NewFallingBlock(1, 4.5, 64, 2.5, 0, 0, 0, registry.BlockStates["minecraft:anvil"], 1)
	minX, minY, minZ, maxX, maxY, maxZ := f.HurtBox()
	if minX != 4.5-FallingHalfWidth || maxX != 4.5+FallingHalfWidth {
		t.Errorf("x box = [%g, %g], want the body's own", minX, maxX)
	}
	if minY != 64 || maxY != 64+FallingHeight {
		t.Errorf("y box = [%g, %g], want [64, %g]", minY, maxY, 64+FallingHeight)
	}
	if minZ != 2.5-FallingHalfWidth || maxZ != 2.5+FallingHalfWidth {
		t.Errorf("z box = [%g, %g], want the body's own", minZ, maxZ)
	}
}

// TestEntityDropsRuleOffSilencesEveryDropSite checks that a body giving its
// block up drops nothing while entity_drops is false: the lifetime end and the
// failed landing both.
func TestEntityDropsRuleOffSilencesEveryDropSite(t *testing.T) {
	sand := registry.BlockStates["minecraft:sand"]
	for _, noDrops := range []bool{false, true} {
		w := newTestWorld()
		w.noDrops = noDrops
		want := 1
		if noDrops {
			want = 0
		}

		f := NewFallingBlock(1, 4.5, 1300, 2.5, 0, 0, 0, sand, 1)
		var events []FallingEvent
		for i := 0; i < fallingLifetimeTicks+1 && !f.Dead; i++ {
			events = f.Step(&w)
		}
		if !f.Dead || len(events) != want {
			t.Fatalf("noDrops=%v lifetime end: events = %v, dead = %v; want %d", noDrops, events, f.Dead, want)
		}

		// A torch is not replaceable, so the landing on it cannot place.
		w.set(4, 64, 2, registry.BlockStates["minecraft:stone"])
		w.set(4, 65, 2, registry.BlockStates["minecraft:torch"])
		// The landing breaks the body as well as dropping it.
		f = NewFallingBlock(2, 4.5, 70, 2.5, 0, 0, 0, sand, 1)
		events = nil
		for i := 0; i < 40 && !f.Dead; i++ {
			events = f.Step(&w)
		}
		if !f.Dead || len(events) != 2*want {
			t.Fatalf("noDrops=%v failed landing: events = %v, dead = %v; want %d", noDrops, events, f.Dead, want)
		}
	}
}

func TestFallingAirDisappearsBeforeStepping(t *testing.T) {
	for _, state := range []uint16{
		registry.BlockAir.DefaultState(), registry.BlockCaveAir.DefaultState(), registry.BlockVoidAir.DefaultState(), 65535,
	} {
		f := NewFallingBlock(1, 4.5, 90, 2.5, 0, 0, 0, state, 1)
		events := f.Step(nil)
		if !f.Dead || f.Time != 0 || len(events) != 0 {
			t.Errorf("state %d: dead=%v time=%d events=%v, want immediate discard", state, f.Dead, f.Time, events)
		}
	}
}

// TestALandingInWaterIsWaterlogged checks that a waterloggable block landing
// in a water source keeps its other properties and gains waterlogged, and
// that flowing water does not waterlog it.
// vanilla: FallingBlockEntity.tick
func TestALandingInWaterIsWaterlogged(t *testing.T) {
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "top", "waterlogged": "false"})
	for _, tc := range []struct {
		water map[string]string
		want  bool
	}{{map[string]string{"level": "0"}, true}, {map[string]string{"level": "1"}, false}} {
		w := newTestWorld()
		w.set(0, 63, 0, registry.BlockStates["minecraft:stone"])
		w.set(0, 64, 0, registry.MustState("minecraft:water", tc.water))
		_, events := fallTick(t, &w, 0.5, 66, 0.5, slab)
		if len(events) != 1 || events[0].Kind != FallingLanded {
			t.Fatalf("level %s: events = %v, want one landing", tc.water["level"], events)
		}
		if got := registry.StateBool(events[0].State, registry.PropWaterlogged); got != tc.want {
			t.Errorf("level %s: waterlogged = %v, want %v", tc.water["level"], got, tc.want)
		}
		if got := registry.StateValue(events[0].State, registry.PropType); got != "top" {
			t.Errorf("level %s: type = %q, want top kept", tc.water["level"], got)
		}
	}
}

// BenchmarkFallingBlockLandsInWater steps a waterloggable block from just
// above a water source until it lands.
func BenchmarkFallingBlockLandsInWater(b *testing.B) {
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "top", "waterlogged": "false"})
	w := newTestWorld()
	w.set(0, 63, 0, registry.BlockStates["minecraft:stone"])
	w.set(0, 64, 0, registry.MustState("minecraft:water", map[string]string{"level": "0"}))
	b.ReportAllocs()
	for range b.N {
		f := NewFallingBlock(1, 0.5, 66, 0.5, 0, 0, 0, slab, 1)
		for i := 0; !f.Dead && i < 1000; i++ {
			f.Step(&w)
		}
		if !f.Dead {
			b.Fatal("the body never came to rest")
		}
	}
}
