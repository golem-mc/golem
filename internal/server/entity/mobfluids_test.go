package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob's fluid machinery as ordinary unit tests, the way the item
// simulation's water is run: each case steps a mob against a world that is
// nothing but the blocks it sets, and asserts on the numbers the vanilla
// parts run on — the depth the float goal draws its kicks at, the hops the
// jump block turns them into, the push a current gives. The black-box
// tests in internal/server/*_test.go remain the proof of a mob the server actually
// spawns and shows around.

// pool builds the standing water a bobbing test swims in: a five-by-five
// room of water six blocks deep on a stone floor, walled in so nobody
// wanders out of it, and the height of the water's surface, the top
// source cell's own eight ninths over its floor.
func pool(t *testing.T) (w testWorld, surface float64) {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w = newTestWorld()
	floor(w, -3, -3, 3, 3, 63, stone)
	for y := int32(64); y <= 69; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	for y := int32(64); y <= 72; y++ {
		for i := int32(-3); i <= 3; i++ {
			w.set(i, y, -3, stone)
			w.set(i, y, 3, stone)
			w.set(-3, y, i, stone)
			w.set(3, y, i, stone)
		}
	}
	return w, 69 + fluidOwnHeight(fluidCellOf(water))
}

// TestWaterPlantCellsReadAsWater checks the fluid read the mobs' breath and
// swimming run on: a kelp or seagrass cell holds a water source although the
// block carries no waterlogged property.
func TestWaterPlantCellsReadAsWater(t *testing.T) {
	for _, name := range []string{"minecraft:kelp", "minecraft:kelp_plant", "minecraft:seagrass", "minecraft:tall_seagrass"} {
		if got := fluidCellOf(defaultState(t, name)); got.kind != fluidWater {
			t.Errorf("fluidCellOf(%s) = kind %d, want a water source", name, got.kind)
		}
	}
}

// TestASheepBobsAtTheDepthTheFloatGoalDraws puts a sheep in deep water.
// The float goal's kicks hold it to the band the
// jump threshold measures — the feet dip under the surface while the water
// reads deeper than the eyes' four tenths, and the coast of the kicks
// carries them back up, but never out of the water: the vanilla sheep
// bobs with its feet between a quarter and nearly half a block under the
// surface and never once above it. A body that rides the surface never
// dips; a body that sinks never rises; both fail the band.
func TestASheepBobsAtTheDepthTheFloatGoalDraws(t *testing.T) {
	w, surface := pool(t)

	sheep := NewMob(TypeSheep, 1, 0.5, 66, 0.5, 1)
	dipped, crested := false, false
	for i := range 700 {
		sheep.Tick(w, nil)
		if i < 60 {
			continue // the settling before the band is read
		}
		if sheep.Y <= surface-0.35 {
			dipped = true
		}
		if sheep.Y >= surface-0.35 {
			crested = true
		}
		if sheep.Y > surface {
			t.Fatalf("the sheep's feet broke the water's skin at %v, want a bob under the surface's own %v", sheep.Y, surface)
		}
	}
	if !dipped {
		t.Errorf("the sheep's feet never dipped %v under the surface, want the kicks' trough", 0.35)
	}
	if !crested {
		t.Errorf("the sheep's feet never rose within %v of the surface, want the kicks' crest", 0.35)
	}
}

// TestAChickenWadesWhereItsEyesReadShallowWater checks the threshold
// against the water's depth: a puddle a ninth of a block deep is under
// the four tenths an adult chicken's eyes draw, and the adult wades it —
// no kick, no hop, the feet where they started. The chick, whose eyes
// sit under the four tenths themselves, reads the same puddle as deep
// water and paddles out of it.
func TestAChickenWadesWhereItsEyesReadShallowWater(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	flow, ok := registry.FindState("minecraft:water", map[string]string{"level": "7"})
	if !ok {
		t.Fatal("the block registry has no level-seven flowing water")
	}
	w.set(0, 64, 0, flow)

	adult := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 1)
	highest := 0.0
	for range 300 {
		adult.Tick(w, nil)
		if adult.Y > highest {
			highest = adult.Y
		}
	}
	if highest > 64.001 {
		t.Errorf("the adult chicken's feet rose to %v in a puddle its own threshold wades, want them where they started", highest)
	}

	baby := NewMob(TypeChicken, 2, 0.5, 64, 0.5, 1)
	baby.SetBaby(true)
	bounced := false
	for range 300 {
		baby.Tick(w, nil)
		if baby.Y > 64.12 {
			bounced = true
		}
	}
	if !bounced {
		t.Errorf("the chick's feet never left the puddle, want the kicks its own low eyes ask for")
	}
}

// TestACreeperBobsWhereAZombieWalksTheBottom checks the registration
// vanilla gives the float goal: the creeper's class registers it at one,
// the zombie's nowhere at all — the creeper paddles at the depth the
// threshold draws, and the zombie stands on the bottom of the pool.
func TestACreeperBobsWhereAZombieWalksTheBottom(t *testing.T) {
	w, surface := pool(t)

	creeper := NewMob(TypeCreeper, 1, 0.5, 66, 0.5, 1)
	dipped := false
	for i := range 700 {
		creeper.Tick(w, nil)
		if i < 60 {
			continue
		}
		if creeper.Y <= surface-0.35 {
			dipped = true
		}
		if creeper.Y < 64.5 {
			t.Fatalf("the creeper sank to %v, want a body the float goal holds up", creeper.Y)
		}
	}
	if !dipped {
		t.Errorf("the creeper never dipped %v under the surface, want the bob its own goal draws", 0.35)
	}

	zombie := NewMob(TypeZombie, 2, 0.5, 66, 0.5, 1)
	for range 700 {
		zombie.Tick(w, nil)
	}
	if zombie.Y > 64.001 {
		t.Errorf("the zombie floats at %v, want it standing on the pool floor its class gives it no goal against", zombie.Y)
	}
}

// TestANoGravityMobDropsTheFluidGravity holds a NoGravity zombie — a body
// with no float goal of its own — in deep water and deep lava: the travel
// branches subtract no gravity term, where a body still acted on takes the
// water's sixteenth and the deep lava's quarter of a tick's fall.
func TestANoGravityMobDropsTheFluidGravity(t *testing.T) {
	w, _ := pool(t)
	water := NewMob(TypeZombie, 1, 0.5, 66, 0.5, 1)
	water.NoGravity = true
	water.updateMobFluids(w)
	water.travelInFluid(w)
	if water.VY != 0 {
		t.Errorf("vy after a tick in water = %v, want nought, the gravity term dropped", water.VY)
	}

	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	lw := newTestWorld()
	floor(lw, -3, -3, 3, 3, 63, stone)
	for y := int32(64); y <= 69; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				lw.set(x, y, z, lava)
			}
		}
	}
	molten := NewMob(TypeZombie, 2, 0.5, 66, 0.5, 1)
	molten.NoGravity = true
	molten.updateMobFluids(lw)
	molten.travelInFluid(lw)
	if molten.VY != 0 {
		t.Errorf("vy after a tick in lava = %v, want nought, the gravity term dropped", molten.VY)
	}
}

// TestAMobInWaterTakesTheLaddersClimbAgainstAWall checks
// LivingEntity.travelInWater's climb: a body pressed against a wall with a
// ladder at its feet takes the two tenths of a block a tick the climb sets
// before the water's drag, where the same body with no ladder keeps the
// water's own fall. The roof over the shaft leaves the swim-out hop nowhere
// to go, so the climb's rise is the only vertical left.
func TestAMobInWaterTakesTheLaddersClimbAgainstAWall(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	ladder, ok := registry.FindState("minecraft:ladder", map[string]string{"facing": "west", "waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged west-facing ladder")
	}

	run := func(withLadder bool) float64 {
		ww := newTestWorld()
		floor(ww, -2, -2, 2, 2, 63, stone)
		for y := int32(64); y <= 65; y++ {
			ww.set(0, y, 0, water)
			ww.set(1, y, 0, stone)
		}
		ww.set(0, 66, 0, stone)
		if withLadder {
			ww.set(0, 64, 0, ladder)
		}
		m := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
		m.VX = 1 // pushed east into the wall
		m.updateMobFluids(ww)
		m.travelInFluid(ww)
		return m.VY
	}
	want := climbUp*defaultWaterSlowDown - mobGravity/16
	if got := run(true); !closeEnough(got, want) {
		t.Errorf("vy with a ladder = %v, want the climb's %v", got, want)
	}
	if got := run(false); got >= 0 {
		t.Errorf("vy without a ladder = %v, want the water's own fall, no climb", got)
	}
}

// TestASpiderInWaterKeepsTheWaterFallAtALadder checks Spider.onClimbable's
// override: a spider whose climbing flag is clear keeps the water's own fall
// pressed against a wall with a ladder at its feet, where every other mob
// takes the ladder's climb. The roof leaves the swim-out hop nowhere to go.
func TestASpiderInWaterKeepsTheWaterFallAtALadder(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	ladder, ok := registry.FindState("minecraft:ladder", map[string]string{"facing": "west", "waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged west-facing ladder")
	}

	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	w.set(0, 64, 0, ladder)
	w.set(1, 64, 0, stone) // the wall east of the spider
	w.set(0, 65, 0, stone) // the roof, the swim-out hop's nowhere

	spider := aimless(NewMob(TypeSpider, 1, 0.2, 64, 0.5, 1))
	spider.VX = 1 // pushed east into the wall
	spider.updateMobFluids(w)
	spider.travelInFluid(w)
	want := -mobGravity / 16
	if !closeEnough(spider.VY, want) {
		t.Errorf("vy with a ladder = %v, want the water's own fall %v, the climb no spider's", spider.VY, want)
	}
}

// TestASwimmerPressedAgainstALedgeHopsOutOfTheWater checks the climb-out:
// a body swimming against a wall whose top is the water's own surface,
// kicked by the float goal while it presses, takes the three-tenths rise
// the jump-out-of-water climb gives — the hop that carries it over.
func TestASwimmerPressedAgainstALedgeHopsOutOfTheWater(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	w.set(0, 64, 2, water)
	w.set(1, 64, 2, stone) // the ledge, its top at the water's surface

	sheep := aimless(NewMob(TypeSheep, 1, 0.5, 64, 2.5, 1))
	sheep.ai.wire(newFloatGoal(sheep))
	sheep.nav.climbTo, sheep.nav.climbSet = Waypoint{4, 64, 2}, true
	sheep.nav.speedFactor = 1

	hopped := false
	for range 20 {
		sheep.Tick(w, nil)
		if sheep.VY == 0.3 {
			hopped = true
			break
		}
	}
	if !hopped {
		t.Errorf("the pressed swimmer's velocity never became the climb-out's %v, want the hop at the wall", 0.3)
	}
}

// fallingColumn builds a falling column of the given fluid at x=z=0 from
// y=60 to 63, with a stone wall beside it on +x when wall is set.
// vanilla: FlowingFluid.getFlow adds the downward share only beside a
// solid face.
func fallingColumn(t *testing.T, fluid string, wall bool) testWorld {
	t.Helper()
	falling, ok := registry.FindState(fluid, map[string]string{"level": "8"})
	if !ok {
		t.Fatalf("the block registry has no falling %s state", fluid)
	}
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	for y := int32(60); y <= 63; y++ {
		w.set(0, y, 0, falling)
		if wall {
			w.set(1, y, 0, stone)
		}
	}
	w.set(0, 59, 0, stone)
	return w
}

// TestAFallingWaterColumnCarriesAMobDown checks the push the fluid
// interaction gives a body in a waterfall: against a wall the current
// points down the fall and carries the body with it; in open air the
// column has no current, and the body sinks only as still water lets it.
func TestAFallingWaterColumnCarriesAMobDown(t *testing.T) {
	walled := aimless(NewMob(TypeSheep, 1, 0.5, 63, 0.5, 1))
	walled.Tick(fallingColumn(t, "minecraft:water", true), nil)
	if walled.VY >= 0 {
		t.Errorf("vy after one tick in a falling column against a wall = %v, want the current carrying it down", walled.VY)
	}
	if walled.Y >= 63 {
		t.Errorf("y after one tick in a falling column against a wall = %v, want it carried below where it started", walled.Y)
	}

	free := aimless(NewMob(TypeSheep, 2, 0.5, 63, 0.5, 1))
	free.updateMobFluids(fallingColumn(t, "minecraft:water", false))
	if free.VX != 0 || free.VY != 0 || free.VZ != 0 {
		t.Errorf("velocity after the fluid pass in a free-standing column = %v %v %v, want no current", free.VX, free.VY, free.VZ)
	}
}

// TestAnIceWallGivesAFallingColumnNoPull: ice is never a solid face to a
// flow (vanilla: FlowingFluid.isSolidFace, IceBlock), so a column beside
// it has no downward current.
func TestAnIceWallGivesAFallingColumnNoPull(t *testing.T) {
	for _, name := range []string{"minecraft:ice", "minecraft:frosted_ice"} {
		w := fallingColumn(t, "minecraft:water", false)
		ice := defaultState(t, name)
		for y := int32(60); y <= 64; y++ {
			w.set(1, y, 0, ice)
		}
		sheep := NewMob(TypeSheep, 1, 0.5, 63, 0.5, 1)
		sheep.updateMobFluids(w)
		if sheep.VY != 0 {
			t.Errorf("%s: vy %v beside an ice wall, want no current", name, sheep.VY)
		}
	}
}

// TestAFallingColumnPullsDownOnlyBesideASturdyFace: the wall that gives a
// falling column its downward share is one whose face toward the step is
// sturdy, not one whose collision reaches the block top: a run of iron bars
// along the column gives none, soul sand (0.875 tall) gives it.
// vanilla: FlowingFluid.isSolidFace
func TestAFallingColumnPullsDownOnlyBesideASturdyFace(t *testing.T) {
	bars, ok := registry.FindState("minecraft:iron_bars", map[string]string{"north": "true", "south": "true", "east": "false", "west": "false", "waterlogged": "false"})
	if !ok {
		t.Fatal("the block registry has no north-south iron_bars state")
	}
	for _, c := range []struct {
		name  string
		state uint16
		pull  bool
	}{
		{"iron_bars[north=true,south=true]", bars, false},
		{"soul_sand", defaultState(t, "minecraft:soul_sand"), true},
	} {
		w := fallingColumn(t, "minecraft:water", false)
		for y := int32(60); y <= 64; y++ {
			w.set(1, y, 0, c.state)
		}
		sheep := NewMob(TypeSheep, 1, 0.5, 63, 0.5, 1)
		sheep.updateMobFluids(w)
		if pulled := sheep.VY < 0; pulled != c.pull {
			t.Errorf("%s: vy %v beside the column, want a downward pull %v", c.name, sheep.VY, c.pull)
		}
	}
}

// TestAMobInALavaCauldronBurns stands a cow in a lava cauldron: it is lit
// for the lava's fifteen seconds and bitten by the lava, though no fluid is
// there.
// vanilla: LavaCauldronBlock.entityInside
func TestAMobInALavaCauldronBurns(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:lava_cauldron"))
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.NoAI = true
	var bitten bool
	for _, ev := range cow.Tick(w, nil) {
		bitten = bitten || ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeLava
	}
	if !bitten || cow.FireTicks < fireTicksLava-1 {
		t.Errorf("a tick in a lava cauldron: bitten %v, fire %d, want the lava's bite and %d", bitten, cow.FireTicks, fireTicksLava)
	}
}

// TestATurtleStandsOutOfEveryCurrent: Turtle.isPushedByFluid is false, so a
// falling column of water or lava leaves its velocity alone while a sheep
// is carried down.
func TestATurtleStandsOutOfEveryCurrent(t *testing.T) {
	for _, fluid := range []string{"minecraft:water", "minecraft:lava"} {
		w := fallingColumn(t, fluid, true)
		sheep := NewMob(TypeSheep, 1, 0.5, 63, 0.5, 1)
		sheep.updateMobFluids(w)
		if sheep.VY >= 0 {
			t.Fatalf("%s: a sheep's vy %v, want the current carrying it down", fluid, sheep.VY)
		}
		turtle := NewMob(TypeTurtle, 2, 0.5, 63, 0.5, 1)
		turtle.updateMobFluids(w)
		if turtle.VX != 0 || turtle.VY != 0 || turtle.VZ != 0 {
			t.Errorf("%s: a turtle's velocity %v %v %v, want it untouched", fluid, turtle.VX, turtle.VY, turtle.VZ)
		}
	}
}

func TestMobWaterSlowDown(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want float64
	}{
		{TypePolarBear, 0.98},
		{TypeSkeletonHorse, 0.96},
		{TypeCow, 0.8},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			m := NewMob(tc.kind, 1, 0.5, 66, 0.5, 1)
			if got := m.waterSlowDown(); got != tc.want {
				t.Errorf("waterSlowDown() = %v, want %v", got, tc.want)
			}
			w, _ := pool(t)
			m.updateMobFluids(w)
			if !m.InWater() {
				t.Fatal("mob must start in water")
			}
			m.VX, m.VY, m.VZ = 1, 0, -0.5
			m.travelInWater(w, mobGravity, true, m.Y)
			want := float64(float32(tc.want)) // the drag is a float
			if !closeEnough(m.VX, want) || !closeEnough(m.VZ, -0.5*want) {
				t.Errorf("horizontal velocity = (%v, %v), want (%v, %v)", m.VX, m.VZ, want, -0.5*want)
			}
			if !closeEnough(m.X, 1.5) || !closeEnough(m.Z, 0) {
				t.Errorf("position = (%v, %v), want (1.5, 0): drag applies after movement", m.X, m.Z)
			}
			if !closeEnough(m.VY, -mobGravity/16) {
				t.Errorf("vertical velocity = %v, want %v", m.VY, -mobGravity/16)
			}
		})
	}
}

// TestThePourOverAnEdgeFollowsTheBlocksFluidFlowTag: the dry neighbour a
// flow pours past is tested with #blocks_fluid_flow, so a sign (no
// collision) stops the pull and a carpet (a thin collision) does not.
// vanilla: FlowingFluid.getFlow
func TestThePourOverAnEdgeFollowsTheBlocksFluidFlowTag(t *testing.T) {
	flowing, ok := registry.FindState("minecraft:water", map[string]string{"level": "2"})
	if !ok {
		t.Fatal("the block registry has no level-2 water state")
	}
	water := defaultState(t, "minecraft:water")
	flowX := func(neighbour uint16) float64 {
		w := newTestWorld()
		w.set(0, 64, 0, flowing)
		if neighbour != 0 {
			w.set(1, 64, 0, neighbour)
		}
		w.set(1, 63, 0, water)
		fx, _, _ := fluidFlowAt(w, 0, 64, 0, fluidCellOf(flowing))
		return fx
	}
	air := flowX(0)
	if air <= 0 {
		t.Fatalf("x flow toward an air edge = %v, want positive", air)
	}
	if got := flowX(defaultState(t, "minecraft:oak_sign")); got != 0 {
		t.Errorf("x flow toward an oak_sign edge = %v, want 0", got)
	}
	if got := flowX(defaultState(t, "minecraft:white_carpet")); got != air {
		t.Errorf("x flow toward a white_carpet edge = %v, want %v as with air", got, air)
	}
}

// TestWaterMovementEfficiencyAndDolphinsGraceSpeedAMobInWater: a zombie
// walking the floor of a pool takes the water's 0.02 push and 0.8 drag at
// water_movement_efficiency 0, its own pace and the 0.546 drag at 1, and
// Dolphin's Grace's 0.96 drag over the water's own.
// vanilla: LivingEntity.travelInWater
func TestWaterMovementEfficiencyAndDolphinsGraceSpeedAMobInWater(t *testing.T) {
	t.Parallel()
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -3, -3, 3, 3, 63, stone)
	for y := int32(64); y <= 66; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	run := func(efficiency float64, grace bool) float64 {
		m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeWaterMovementEfficiency, efficiency)
		if grace {
			m.effects.Add(effect.Instance{ID: registry.MobEffectDolphinsGrace, Duration: 100})
		}
		m.OnGround = true
		m.walk.dirX, m.walk.dirZ, m.walk.speed = 0, 1, 0.23
		m.updateMobFluids(w)
		if !m.InWater() {
			t.Fatal("the zombie must start in water")
		}
		m.travelInWater(w, mobGravity, true, m.Y)
		return math.Hypot(m.VX, m.VZ)
	}
	for _, tc := range []struct {
		name       string
		efficiency float64
		grace      bool
		want       float64
	}{
		{"plain", 0, false, 0.23 * 0.98 * 0.02 * 0.8},
		{"efficiency 1", 1, false, 0.23 * 0.98 * 0.23 * 0.54600006},
		{"dolphins grace", 0, true, 0.23 * 0.98 * 0.02 * 0.96},
	} {
		// float32 arithmetic, as vanilla's
		if got := run(tc.efficiency, tc.grace); math.Abs(got-tc.want) > 1e-7 {
			t.Errorf("%s: horizontal speed after a tick = %v, want %v", tc.name, got, tc.want)
		}
	}
}
