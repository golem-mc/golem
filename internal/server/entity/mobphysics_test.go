package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The walk and shove arithmetic as unit tests of their own: each drives a
// mob against a world that is nothing but the floor it sets and asserts on
// the plain numbers the vanilla parts run on.
// TestAMobWalksAtThePaceItsAttributeSquaresTo holds the walk to vanilla's
// travel arithmetic: the walk's acceleration is the pace the move control
// set multiplied by itself, the body moves with the whole of what it
// carries, and the ground's friction takes its share off after the move —
// a zombie's 0.23 attribute walking out at 0.11654 blocks a tick, the
// figure a vanilla server's own chase traces at.
func TestAMobWalksAtThePaceItsAttributeSquaresTo(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	zombie := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	// A player within reach keeps the hostile despawn from taking the
	// walker before its walk is measured; its goals are stripped, so the
	// body goes only where the test points it, far short of the player.
	watcher := Player{X: 10.5, Y: 64, Z: 0.5}
	target := 20.5
	zombie.seekTo(w, target, 64, 0.5, 1.0, 0)
	for range 30 {
		zombie.Tick(w, []Player{watcher})
	}
	// The steady pace, measured over the walk that follows the ramp-in.
	was := zombie.X
	for range 40 {
		zombie.Tick(w, []Player{watcher})
	}
	perTick := (zombie.X - was) / 40
	if want := 0.23 * 0.23 / (1 - slipperiness*airDrag); math.Abs(perTick-want) > 0.002 {
		t.Errorf("the zombie's steady walk = %v a tick, want the vanilla %v", perTick, want)
	}
}

// TestAMobIdleVelocityDecaysAfterTheMove holds the knock a shove dealt to
// spend itself the vanilla way: the body moves with what it was given, and
// the friction takes its share off what is left — the first move after a
// hit carrying the shove whole.
func TestAMobIdleVelocityDecaysAfterTheMove(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	// The settle that lands it, the friction reading its feet after: the
	// first tick's gravity only comes off after its move.
	cow.Tick(w, nil)
	cow.Tick(w, nil)
	cow.VX = 0.4
	was := cow.X
	cow.Tick(w, nil)
	if moved := cow.X - was; math.Abs(moved-0.4) > 0.01 {
		t.Errorf("the first move after a shove carried %v, want the shove's own 0.4", moved)
	}
	if decayed := cow.VX; math.Abs(decayed-0.4*(slipperiness*airDrag)) > 0.001 {
		t.Errorf("the velocity after the move = %v, want the friction's share %v", decayed, 0.4*(slipperiness*airDrag))
	}
}

// TestBlockExtentReadsPowderSnowEmpty holds the contextless read: powder
// snow has no collision of its own, only the per-body shape powderSnowBoxes
// gives.
func TestBlockExtentReadsPowderSnowEmpty(t *testing.T) {
	if got := blockExtent(defaultState(t, "minecraft:powder_snow")); got != 0 {
		t.Errorf("blockExtent(powder snow) = %v, want 0", got)
	}
}

// TestBlockExtentReadsTheModelledShapes holds the heights the world's own
// collision boxes give: the families with modelled shapes reach their
// boxes' tops, everything else keeps the generated table's per-state
// word.
func TestBlockExtentReadsTheModelledShapes(t *testing.T) {
	state := func(name string, props map[string]string) uint16 {
		t.Helper()
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  float64
	}{
		{"minecraft:oak_fence", nil, 1.5},
		{"minecraft:cobblestone_wall", nil, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "false"}, 1.5},
		{"minecraft:oak_fence_gate", map[string]string{"open": "true"}, 0},
		{"minecraft:chest", nil, 14.0 / 16},
		{"minecraft:lantern", nil, 9.0 / 16},
		{"minecraft:candle", map[string]string{"candles": "3"}, 6.0 / 16},
		{"minecraft:iron_chain", map[string]string{"axis": "x"}, 9.5 / 16},
		{"minecraft:creeper_head", nil, 0.5},
		// The unmodelled blocks keep the generated table's own word.
		{"minecraft:stone", nil, 1},
		{"minecraft:farmland", nil, 15.0 / 16},
		{"minecraft:white_carpet", nil, 1.0 / 16},
		{"minecraft:soul_sand", nil, 14.0 / 16},
		{"minecraft:torch", nil, 0},
	} {
		if got := blockExtent(state(tc.name, tc.props)); got != tc.want {
			t.Errorf("blockExtent(%s %v) = %v, want %v", tc.name, tc.props, got, tc.want)
		}
	}
}

// TestANoAIBodyStillDropsADriftVelocity holds the zeroing vanilla's aiStep
// runs ahead of its AI gate to a NoAI body: the drift under three
// thousandths goes to nothing though no travel ever runs, while a shove
// past it — a punch's knockback — sits on in the Motion a save carries.
func TestANoAIBodyStillDropsADriftVelocity(t *testing.T) {
	w := newTestWorld()

	drifting := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	drifting.NoAI = true
	drifting.VX, drifting.VY, drifting.VZ = 0.002, 0.001, -0.002
	drifting.Tick(w, nil)
	if drifting.VX != 0 || drifting.VY != 0 || drifting.VZ != 0 {
		t.Errorf("the NoAI cow's velocity = (%v, %v, %v), want the drift zeroed",
			drifting.VX, drifting.VY, drifting.VZ)
	}

	shoved := aimless(NewMob(TypeCow, 2, 0.5, 64, 0.5, 1))
	shoved.NoAI = true
	shoved.VX = 0.4
	shoved.Tick(w, nil)
	if shoved.VX != 0.4 {
		t.Errorf("the shoved NoAI cow's velocity x = %v, want the shove kept whole", shoved.VX)
	}
}

// TestAMobAndAnItemRestOnAFence holds the fall onto a shape that reaches
// into the cell above its own: an oak fence's collision tops out 24
// sixteenths above its floor, so a body whose feet pass through that upper
// half rests on the shape's top instead of sinking deeper and being shoved
// out sideways.
func TestAMobAndAnItemRestOnAFence(t *testing.T) {
	fence := defaultState(t, "minecraft:oak_fence")
	w := newTestWorld()
	w.set(0, -61, 0, fence)

	cow := aimless(NewMob(TypeCow, 1, 0.5, -57.4, 0.5, 1))
	it := &Item{X: 0.5, Y: -57 + halfSize, Z: 0.5, Health: 5}
	for range 80 {
		cow.Tick(w, nil)
		it.Step(w, nil)
	}
	if cow.Y != -59.5 || !cow.OnGround {
		t.Errorf("the cow rests at y = %v (on ground %v), want the fence top -59.5", cow.Y, cow.OnGround)
	}
	if it.Y != -59.5+halfSize || !it.OnGround {
		t.Errorf("the item rests at y = %v (on ground %v), want %v on the fence top",
			it.Y, it.OnGround, -59.5+halfSize)
	}
}

// TestBodiesLandOnGroundBelowYZero holds the landing and the step-up to the
// floor under them where every block top is negative, as in a vanilla
// superflat world (#95): an item, an orb and a cow dropped over grass at
// y=-61 rest on it, and a cow walking into a slab there climbs it.
func TestBodiesLandOnGroundBelowYZero(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, -61, stone)

	it := &Item{X: 0.5, Y: -58 + halfSize, Z: 0.5, Health: 5}
	o := &Orb{X: 0.5, Y: -58, Z: 0.5, Value: 1}
	cow := aimless(NewMob(TypeCow, 1, 0.5, -58, 0.5, 1))
	for range 60 {
		it.Step(w, nil)
		o.Step(w, nil, nil)
		cow.Tick(w, nil)
	}
	if it.Y != -60+halfSize || !it.OnGround {
		t.Errorf("item y = %v (on ground %v), want %v on the grass", it.Y, it.OnGround, -60+halfSize)
	}
	if o.Y != -60 {
		t.Errorf("orb y = %v, want -60 on the grass", o.Y)
	}
	if cow.Y != -60 || !cow.OnGround {
		t.Errorf("cow y = %v (on ground %v), want -60 on the grass", cow.Y, cow.OnGround)
	}

	w.set(1, -60, 0, slabState(t))
	cow.moveHorizontal(w, cow.collideCtx(), 0.6, 0)
	if cow.Y != -59.5 {
		t.Errorf("cow y after walking into a slab = %v, want the step-up onto it at -59.5", cow.Y)
	}
}

// TestAnItemInANoCollisionPlantIsNotPushed lays a stack inside a sweet
// berry bush's cell: the bush carries no collision boxes, so the stack is
// not buried and no escape shoves it sideways.
func TestAnItemInANoCollisionPlantIsNotPushed(t *testing.T) {
	w := newTestWorld()
	w.set(-1, -61, 3, defaultState(t, "minecraft:stone"))
	w.set(-1, -60, 3, defaultState(t, "minecraft:sweet_berry_bush"))

	it := &Item{X: -0.5, Y: -59.575, Z: 3.4, Health: 5}
	for range 40 {
		it.Step(w, nil)
	}
	if it.X != -0.5 || it.Z != 3.4 {
		t.Errorf("the item in the bush moved to (%v, %v), want it left where it lay", it.X, it.Z)
	}
}

// TestAnItemBesideAFencePostFallsPastIt drops a stack inside the post's own
// cell but clear of the four-pixel post: the box only meets the post's
// column, not the post, so it falls to the floor below and settles there.
func TestAnItemBesideAFencePostFallsPastIt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, defaultState(t, "minecraft:oak_fence"))

	it := &Item{X: 0.2, Y: 66, Z: 0.5, Health: 5}
	lowest := it.Y
	roseAgain := false
	for range 100 {
		it.Step(w, nil)
		if it.Y < lowest-1e-9 {
			lowest, roseAgain = it.Y, false
		} else if it.Y > lowest+1e-9 {
			roseAgain = true
		}
	}
	if it.Y != 63+halfSize || !it.OnGround {
		t.Errorf("the item beside the post rests at %v (on ground %v), want %v on the floor",
			it.Y, it.OnGround, 63+halfSize)
	}
	if roseAgain {
		t.Error("the item rose again after falling, want it to settle once")
	}
}

// TestAnItemRestingOnAFencePostStaysOnIt drops a stack straight onto the
// post: it rests on the post's 24-sixteenth top and the block it stands in
// does not push it off.
func TestAnItemRestingOnAFencePostStaysOnIt(t *testing.T) {
	w := newTestWorld()
	w.set(0, 62, 0, defaultState(t, "minecraft:stone"))
	w.set(0, 63, 0, defaultState(t, "minecraft:oak_fence"))

	it := &Item{X: 0.5, Y: 66, Z: 0.5, Health: 5}
	for range 100 {
		it.Step(w, nil)
	}
	if it.Y != 63+1.5+halfSize || !it.OnGround {
		t.Errorf("the item on the post rests at %v (on ground %v), want %v on its top",
			it.Y, it.OnGround, 63+1.5+halfSize)
	}
}

// TestAnArrowFliesPastAFencePost fires a shaft down the post's own cell a
// fifth of a block aside of it: the post is four pixels wide, so the flight
// clears it and goes on.
func TestAnArrowFliesPastAFencePost(t *testing.T) {
	w := newTestWorld()
	w.set(3, 64, 0, defaultState(t, "minecraft:oak_fence"))

	if clipArrowFlight(w, 2.5, 64.5, 0.2, 2, 0, 0).ok {
		t.Error("an arrow a fifth of a block aside the post hit it, want the flight to pass")
	}
}

// TestACowBesideAFenceLineIsNotLifted walks a cow alongside a fence line
// with its box crossing the fence's cells but clear of the posts: the cow
// stays on the floor.
func TestACowBesideAFenceLineIsNotLifted(t *testing.T) {
	fence, ok := registry.FindState("minecraft:oak_fence", map[string]string{
		"north": "true", "south": "true", "east": "false", "west": "false", "waterlogged": "false"})
	if !ok {
		t.Fatal("the block registry has no north-south oak fence")
	}
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, defaultState(t, "minecraft:stone"))
	for z := int32(0); z <= 2; z++ {
		w.set(0, 63, z, fence)
	}

	cow := aimless(NewMob(TypeCow, 1, 1.1, 64, 0.5, 1))
	for range 40 {
		cow.Tick(w, nil)
	}
	if cow.Y != 64 || !cow.OnGround {
		t.Errorf("the cow beside the fence line stands at %v (on ground %v), want 64 on the floor",
			cow.Y, cow.OnGround)
	}
}

// TestAMobSlidesFarthestOnIce holds the drag to the block under the feet:
// a shove of 0.4 spends its slide on each floor at the distance its
// friction times the air's allows, about 3.7 blocks on ice against 0.88 on
// stone.
func TestAMobSlidesFarthestOnIce(t *testing.T) {
	for _, tc := range []struct {
		block    string
		min, max float64
	}{
		{"minecraft:stone", 0.8, 0.95},
		{"minecraft:slime_block", 0.5, 0.65},
		{"minecraft:ice", 3.4, 4.0},
		{"minecraft:blue_ice", 3.85, 4.3},
	} {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, defaultState(t, tc.block))
		cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
		cow.Tick(w, nil)
		cow.Tick(w, nil)
		cow.VX = 0.4
		was := cow.X
		for range 60 {
			cow.Tick(w, nil)
		}
		if got := cow.X - was; got < tc.min || got > tc.max {
			t.Errorf("slide on %s = %v blocks, want within [%v, %v]", tc.block, got, tc.min, tc.max)
		}
	}
}

// TestAMobWalkingOnIceAcceleratesSlowly holds the walk's acceleration to
// getFrictionInfluencedSpeed: on ice the pull is divided down by the cube
// of the friction, so the first ticks of a walk gain less than on stone.
func TestAMobWalkingOnIceAcceleratesSlowly(t *testing.T) {
	gain := func(block string) float64 {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, defaultState(t, block))
		z := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
		z.Tick(w, nil)
		z.Tick(w, nil)
		watcher := Player{X: 10.5, Y: 64, Z: 0.5}
		z.seekTo(w, 20.5, 64, 0.5, 1.0, 0)
		z.Tick(w, []Player{watcher})
		return z.VX
	}
	stone, ice := gain("minecraft:stone"), gain("minecraft:ice")
	if !(ice < stone*0.5 && ice > 0) {
		t.Errorf("first-tick speed on ice = %v, stone = %v; want ice under half of stone's", ice, stone)
	}
}
