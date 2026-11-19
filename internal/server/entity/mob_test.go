package entity

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob simulation as ordinary unit tests: each case steps a mob against
// a world that is nothing but the blocks it sets, and asserts on the plain
// numbers the vanilla parts run on. The black-box tests in
// internal/server/*_test.go remain the proof of a mob the server actually spawns and
// shows around; these are the cheapest checks of the simulation itself.

// floor lays a floor of the given block one can stand on: the cells at y
// hold the block, so the feet of anyone walking on them are at y+1.
func floor(w testWorld, x0, z0, x1, z1, y int32, state uint16) {
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			w.set(x, y, z, state)
		}
	}
}

// groundTo fills the columns of the box from y down to y-8 solid, so a
// target offset below a mob's feet still has a block under it.
func groundTo(w testWorld, x0, z0, x1, z1, y int32, state uint16) {
	for dy := range int32(9) {
		floor(w, x0, z0, x1, z1, y-dy, state)
	}
}

// slab is the bottom-slab state passed, the half-height ledge the step-up
// exists for.
func slabState(t *testing.T) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	if !ok {
		t.Fatal("the block registry has no bottom stone slab")
	}
	return state
}

// itemID is the numeric ID of a named item, so the tests can name what a
// held stack holds.
func itemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// aimless strips a mob's goals, so a test can drive a walk of its own and
// have the body go only where the test pointed it.
func aimless(m *Mob) *Mob {
	m.ai = goalSelector{}
	return m
}

// TestAFallingMobFollowsTheVanillaFormulaAndLands checks the two halves of
// the fall: the velocity runs on vanilla's move-then-gravity-then-drag maths
// the while, and the landing ends on the block's own top face with the fall
// banked as far as it went.
func TestAFallingMobFollowsTheVanillaFormulaAndLands(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	first := (0 - mobGravity) * float64(float32(verticalDrag))
	cow.Tick(w, nil)
	if !closeEnough(cow.VY, first) {
		t.Errorf("vy after one tick = %v, want %v", cow.VY, first)
	}
	if !closeEnough(cow.Y, 68.5) {
		t.Errorf("y after one tick = %v, want 68.5, the resting velocity's move", cow.Y)
	}
	cow.Tick(w, nil)
	if !closeEnough(cow.VY, (first-mobGravity)*float64(float32(verticalDrag))) {
		t.Errorf("vy after two ticks = %v, want %v", cow.VY, (first-mobGravity)*float64(float32(verticalDrag)))
	}
	if !closeEnough(cow.Y, 68.5+first) {
		t.Errorf("y after two ticks = %v, want %v", cow.Y, 68.5+first)
	}
	if cow.FallDistance <= 0 {
		t.Errorf("fall distance during the fall = %v, want it banked", cow.FallDistance)
	}

	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if !cow.OnGround {
		t.Fatal("the cow never came to rest on the floor")
	}
	if !closeEnough(cow.Y, 64) {
		t.Errorf("resting y = %v, want the floor's own top face at 64", cow.Y)
	}
	if !closeEnough(cow.VY, first) {
		t.Errorf("resting vy = %v, want the landing's zero less one tick's gravity, %v", cow.VY, first)
	}
	if cow.FallDistance != 0 {
		t.Errorf("fall distance after landing = %v, want it spent", cow.FallDistance)
	}
	// Four and a half blocks down: the first three were free, the rest
	// cost a floor of them — one off the cow's ten, the charged blocks and
	// half floored.
	// vanilla: calculateFallDamage
	if !closeEnough(cow.Health, 9) {
		t.Errorf("health after a %v-block fall = %v, want 9", 68.5-64, cow.Health)
	}
	if !closeEnough(cow.MaxHealth, 10) {
		t.Errorf("max health = %v, want the vanilla 10 of a cow", cow.MaxHealth)
	}

	// A resting mob stays resting: gravity's fraction is caught every tick
	// by the block's top face, and the second banking of the same stand
	// hurts nobody.
	cow.Tick(w, nil)
	if !cow.OnGround || !closeEnough(cow.Y, 64) || !closeEnough(cow.Health, 9) {
		t.Fatalf("a resting mob did not stay resting: y=%v ground=%v health=%v",
			cow.Y, cow.OnGround, cow.Health)
	}
}

// TestAMobCaughtByAClimbLandsUnhurt checks the climb's mercy on the mobs'
// side of it: a body whose feet come into what it climbs has the fall it
// was carrying written off before the move banks its own descent — the
// reset vanilla's handleOnClimbable runs while the body hangs there — so
// the catch itself and the slide down to the block below cost nothing. The
// vine stands in for the climbable cells a body can be in at all, the
// ladders' cells being solid to the physics' whole-block reading of them.
func TestAMobCaughtByAClimbLandsUnhurt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	w.set(0, 64, 0, defaultState(t, "minecraft:vine"))

	// Dropped straight into the vine's own cell, four and a half above
	// the floor it stands on.
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if !cow.OnGround {
		t.Fatal("the cow never came to rest under the vine")
	}
	if !closeEnough(cow.Y, 64) {
		t.Errorf("resting y = %v, want the floor's own top face at 64", cow.Y)
	}
	if !closeEnough(cow.Health, 10) {
		t.Errorf("health after the catch = %v, want the whole 10, the climb having taken the fall back", cow.Health)
	}
}

// TestASpiderFallsThroughAClimb holds Spider.onClimbable's override on the
// dry branch: a spider whose feet pass through a vine cell without touching
// a wall is no climber — it keeps the fall it built instead of having it
// written off each tick in the vine, where every other mob is caught.
func TestASpiderFallsThroughAClimb(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	vine := defaultState(t, "minecraft:vine")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	for y := int32(64); y <= 67; y++ {
		w.set(0, y, 0, vine)
	}

	spider := aimless(NewMob(TypeSpider, 1, 0.5, 69, 0.5, 1))
	banked := 0.0
	for range 40 {
		spider.Tick(w, nil)
		if !spider.OnGround && spider.Y >= 64 && spider.Y < 67 {
			banked = math.Max(banked, spider.FallDistance)
		}
	}
	if !spider.OnGround {
		t.Fatal("the spider never came to rest on the floor")
	}
	if banked < 1 {
		t.Errorf("the falling spider banked %v through the vine, want the fall the climb does not reset", banked)
	}
}

// TestAMobFallingFastThroughAWebLandsUnhurt checks the same mercy for a fall
// swept through a layer rather than caught in it: a body carrying a long fall
// drops through a one-thick web in a single step a block or longer, and the
// fall the web writes off leaves only the short drop to the floor, so the
// landing costs nothing.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING
func TestAMobFallingFastThroughAWebLandsUnhurt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	w.set(0, 66, 0, defaultState(t, "minecraft:cobweb"))

	// Half a block over the web's own cell, falling fast: the step carries it
	// down through the web and onto the floor's top at 64.
	cow := aimless(NewMob(TypeCow, 1, 0.5, 66.5, 0.5, 1))
	cow.FallDistance = 20
	cow.VY = -4
	cow.Tick(w, nil)
	if !cow.OnGround {
		t.Fatal("the cow did not come to rest on the floor")
	}
	if !closeEnough(cow.Y, 64) {
		t.Errorf("resting y = %v, want the floor's own top face at 64", cow.Y)
	}
	if cow.FallDistance != 0 {
		t.Errorf("fall distance after landing = %v, want it spent", cow.FallDistance)
	}
	if !closeEnough(cow.Health, 10) {
		t.Errorf("health after the fall through the web = %v, want the whole 10", cow.Health)
	}
}

// TestAMobStuckInAWebForgetsItsFall holds a mob that drifts down into a
// cobweb: the fall it carried is written off while it is still in the air.
// vanilla: Entity.makeStuckInBlock
func TestAMobStuckInAWebForgetsItsFall(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 60, stone)
	w.set(0, 65, 0, defaultState(t, "minecraft:cobweb"))

	cow := aimless(NewMob(TypeCow, 1, 0.5, 65.5, 0.5, 1))
	cow.FallDistance = 10
	cow.VY = -0.1
	cow.Tick(w, nil)
	if cow.OnGround {
		t.Fatal("the cow reached the floor")
	}
	if cow.FallDistance != 0 {
		t.Errorf("fall distance in the web = %v, want it written off", cow.FallDistance)
	}
}

// TestAMobWalkingIntoAWallStopsAtItsFace drives a cow toward a wall two
// blocks high and checks the collision stops the body at the wall's own
// face, half the cow's width out from it, and keeps it there.
func TestAMobWalkingIntoAWallStopsAtItsFace(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, 6, -2, 12, 2, 63, stone)
	for _, y := range []int32{64, 65, 66} {
		for z := int32(-6); z <= 6; z++ {
			w.set(9, y, z, stone)
		}
	}

	// The wall is west of the cow; a shove west meets its face at x=10.
	cow := aimless(NewMob(TypeCow, 1, 10.5, 64, 0.5, 1))
	cow.VX = -1.0
	for range 40 {
		cow.Tick(w, nil)
		if cow.X > 10.46 || cow.X < 10.43 {
			t.Fatalf("x after %v ticks = %v, want it stopped at the wall's face (10.45)",
				cow.Age, cow.X)
		}
	}
	if !cow.OnGround {
		t.Error("the cow pushing a wall is off the ground, want it standing")
	}
}

// TestAMobStepsUpASlabHeightLedge drives a cow at a floor that rises half a
// block into bottom slabs and checks the walk climbs the ledge without a
// hop — the step-up of vanilla's step height, a half-block being well
// within it — standing on the slabs' own top at the far side.
func TestAMobStepsUpASlabHeightLedge(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	slabs := slabState(t)
	w := newTestWorld()
	floor(w, 0, -2, 12, 2, 63, stone)
	floor(w, 6, -2, 12, 2, 64, slabs)

	cow := aimless(NewMob(TypeCow, 1, 3.5, 64, 0.5, 1))
	target := 10.5
	for range 200 {
		cow.seekTo(w, target, 64, 0.5, strollPace, 0)
		cow.Tick(w, nil)
		if cow.VY > 0 {
			t.Fatal("the cow jumped the ledge, want it stepped up")
		}
		if math.Abs(cow.X-target) < 0.5 {
			break
		}
	}
	if math.Abs(cow.X-target) >= 0.5 {
		t.Fatalf("the cow never reached the slabs: x = %v", cow.X)
	}
	if want := 64 + 0.5; !closeEnough(cow.Y, want) {
		t.Errorf("standing y = %v, want the slabs' own top at %v", cow.Y, want)
	}
}

// TestAMobJumpsUpAOneBlockStep walks a mob at a step one block high —
// open, under a roof two blocks over the step's top, at an outside corner,
// and for the chicken under a roof two blocks over its own floor — and
// holds it on top within five seconds (issues #10 and #90). The jump's
// first tick rises the whole 0.42 of JUMP_STRENGTH, gravity coming off
// after the move as LivingEntity.travelInAir takes it, so the arc peaks
// 1.25 up and clears the step.
func TestAMobJumpsUpAOneBlockStep(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	type scene struct {
		name           string
		kind           MobType
		raised         func(x, z int32) bool
		roof           func(x int32) int32 // the roof's y over a column; nil for none
		startZ, tx, tz float64
	}
	straight := func(x, _ int32) bool { return x >= 6 }
	corner := func(x, z int32) bool { return x >= 6 && z >= 1 }
	overStep := func(int32) int32 { return 67 }
	lowOverFloor := func(x int32) int32 {
		if x < 6 {
			return 66
		}
		return 67
	}
	scenes := []scene{
		{"cow, open step", TypeCow, straight, nil, 0.5, 9.5, 0.5},
		{"cow, roofed step", TypeCow, straight, overStep, 0.5, 9.5, 0.5},
		{"cow, corner", TypeCow, corner, nil, -2.5, 9.5, 4.5},
		{"sheep, open step", TypeSheep, straight, nil, 0.5, 9.5, 0.5},
		{"chicken, open step", TypeChicken, straight, nil, 0.5, 9.5, 0.5},
		{"chicken, low roof", TypeChicken, straight, lowOverFloor, 0.5, 9.5, 0.5},
		{"chicken, corner", TypeChicken, corner, nil, -2.5, 9.5, 4.5},
	}
	for _, s := range scenes {
		t.Run(s.name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -6, -6, 14, 8, 63, stone)
			for x := int32(-6); x <= 14; x++ {
				for z := int32(-6); z <= 8; z++ {
					if s.raised(x, z) {
						w.set(x, 64, z, stone)
					}
					if s.roof != nil {
						w.set(x, s.roof(x), z, stone)
					}
				}
			}

			m := aimless(NewMob(s.kind, 1, 3.5, 64, s.startZ, 1))
			jumped := false
			for range 100 {
				m.seekTo(w, s.tx, 65, s.tz, 1.0, 0)
				wasY := m.Y
				m.Tick(w, nil)
				if !jumped && m.Y > wasY {
					jumped = true
					if rise := m.Y - wasY; math.Abs(rise-0.42) > 1e-6 {
						t.Errorf("the jump's first tick rose %v, want vanilla's whole 0.42", rise)
					}
				}
				if m.OnGround && closeEnough(m.Y, 65) && s.raised(m.cell().X, m.cell().Z) {
					return
				}
			}
			t.Errorf("after 100 ticks the %s is at (%.3f, %.3f, %.3f), want it on top of the step at y=65",
				s.kind, m.X, m.Y, m.Z)
		})
	}
}

// TestAHurtCowPanicsWhileItsHurtIsFresh checks the whole chain of a hurt:
// the hit starts the panic, the panic takes the cow somewhere of its own
// choosing — vanilla picks a walkable spot within reach of the feet and
// keeps the best ground it finds (PanicGoal.findRandomPosition) — and the
// freshness spends after the forty ticks, the legs with it.
func TestAHurtCowPanicsWhileItsHurtIsFresh(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	if !cow.TakeDamage(3, Player{X: 0.5, Y: 64, Z: 1.5}) {
		t.Fatal("the hit did not land")
	}
	if got := cow.lastDamageSource(); got != rules.DamageTypePlayerAttack {
		t.Errorf("the hurt's source after the hit = %q, want the swing's own", got)
	}
	startX, startZ := cow.X, cow.Z
	for range 40 {
		cow.Tick(w, nil)
	}
	if math.Hypot(cow.X-startX, cow.Z-startZ) < 1 {
		t.Errorf("the panicked cow ended %v blocks from the hit, want it well into its flee",
			math.Hypot(cow.X-startX, cow.Z-startZ))
	}

	// The hurt is still fresh at its fortieth tick — vanilla's
	// getLastDamageSource spends it only once the forty are passed — and
	// is spent the tick after.
	if got := cow.lastDamageSource(); got != rules.DamageTypePlayerAttack {
		t.Errorf("the hurt's source after forty ticks = %q, want it still fresh", got)
	}
	cow.Tick(w, nil)
	if got := cow.lastDamageSource(); got != 0 {
		t.Errorf("the hurt's source after forty-one ticks = %q, want it spent", got)
	}

	// The freshness spent and the last flee leg walked out, the panic is
	// done: no further leg starts on it.
	for range 50 {
		cow.Tick(w, nil)
	}
	if panicking(cow) {
		t.Error("the panic outlived the hurt's freshness")
	}
}

// TestABurningCowStopsPanickingOnceItsHurtStales drives a burning cow by
// hand across its whole burn: the bites keep the panic's hurt fresh and
// fresh legs keep being rolled while they do — a roll may fail and leave
// the goal standing a tick, but the fresh bite has it back soon — past the
// burn's own end, by the forty ticks a hurt stays fresh — and once the last
// bite has staled the cow stops panicking for good.
//
// vanilla: PanicGoal.canUse
func TestABurningCowStopsPanickingOnceItsHurtStales(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	floor(w, -80, -80, 80, 80, 63, stone)
	w.set(0, 64, 0, fire)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	// Ten health is about what the whole burn costs, so whether the cow
	// outlives it would hang on the dice; the test is about its legs.
	cow.Health = 100
	burntOut, outAt := false, 0
	legs, lastX, lastZ := 0, 0.0, 0.0
	for tick := 1; tick <= 400 && !burntOut; tick++ {
		cow.Tick(w, nil)
		if cow.FireTicks == 0 {
			burntOut, outAt = true, tick
		}
		// The bite's hurt stands fresh while the burn runs, so the legs
		// keep coming.
		if g := panicOf(cow); !burntOut && tick > 10 && g != nil && (g.fleeX != lastX || g.fleeZ != lastZ) {
			legs++
			lastX, lastZ = g.fleeX, g.fleeZ
		}
	}
	if !burntOut {
		t.Fatal("the cow's burn never ran out")
	}
	if legs < 2 {
		t.Errorf("the burning cow took %d legs, want the fresh bites keeping new ones coming", legs)
	}

	// The last bite's freshness outlives the burn by its forty ticks: a leg
	// is still rolled in that window.
	freshLeg := false
	for i := outAt + 1; i <= outAt+40; i++ {
		cow.Tick(w, nil)
		if g := panicOf(cow); g != nil && (g.fleeX != lastX || g.fleeZ != lastZ) {
			freshLeg = true
			lastX, lastZ = g.fleeX, g.fleeZ
		}
	}
	if !freshLeg {
		t.Error("no leg was rolled while the last bite was fresh, want the freshness holding the panic")
	}

	// The freshness spent and the last leg walked out, the cow stops
	// panicking for good.
	for i := outAt + 41; i <= outAt+131; i++ {
		cow.Tick(w, nil)
	}
	if panicking(cow) {
		t.Error("the panic outlived the last bite's freshness")
	}
	for range 20 {
		cow.Tick(w, nil)
		if panicking(cow) {
			t.Fatal("the stale hurt started a new flee")
		}
	}
}

// TestABurningCowFleesByWhimNotAlongTheOriginLine checks where a burning
// cow flees: the fire's bites record no source, so no leg may run along the
// line away from wherever a long-gone hurt stood — the hurt's where never
// written, that line is the way out of the world origin. Of the legs a
// burning cow takes, some must strike out on an angle of their own.
// vanilla: PanicGoal.findRandomPosition
func TestABurningCowFleesByWhimNotAlongTheOriginLine(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	// A floor far out east, where the origin's line is a due-east flight.
	floor(w, 990, -30, 1030, 30, 63, stone)
	w.set(1000, 64, 0, fire)

	cow := NewMob(TypeCow, 1, 1000.5, 64, 0.5, 1)
	originAngle := math.Atan2(cow.Z, cow.X)
	legs, angles := panicLegs(t, cow, w, 8)
	inLine := 0
	for _, angle := range angles {
		diff := math.Abs(angle - originAngle)
		if diff > math.Pi {
			diff = 2*math.Pi - diff
		}
		// The away branch's own scatter spans 0.8 of a radian either way.
		if diff <= 0.8+1e-9 {
			inLine++
		}
	}
	if inLine == legs {
		t.Errorf("all %d flee legs ran along the line away from the world origin, want a whim of angles", legs)
	}
}

// TestABurningCowKeepsTakingLegsWhileItsBurnRuns pins the panic letting go
// at each walk's end: a leg walked out, the goal stops and the next fresh
// bite starts a new leg, rather than the one walk holding the goal while
// the bites keep the hurt fresh.
func TestABurningCowKeepsTakingLegsWhileItsBurnRuns(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)
	w.set(0, 64, 0, fire)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	if legs, _ := panicLegs(t, cow, w, 3); legs < 3 {
		t.Errorf("the burning cow took %d flee legs, want the fresh bites keeping new ones coming", legs)
	}
}

// TestLookForWaterRefusesACellWithCollision checks the gate vanilla's
// lookForWater opens with: a body whose own cell carries a collision —
// here farmland, a sixteenth short — looks for no water at all, however
// near the pond is.
func TestLookForWaterRefusesACellWithCollision(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	farmland := defaultState(t, "minecraft:farmland")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -2, -2, 4, 2, 63, stone)
	w.set(2, 64, 0, water)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))

	if cell, ok := lookForWater(w, cow, panicWaterRange); !ok || cell != (Waypoint{2, 64, 0}) {
		t.Fatalf("lookForWater on open ground = %v, %v, want the pond at (2,64,0)", cell, ok)
	}
	w.set(0, 64, 0, farmland)
	if cell, ok := lookForWater(w, cow, panicWaterRange); ok {
		t.Errorf("lookForWater from a cell with collision found %v, want none", cell)
	}
}

// TestLookForWaterKeepsToTheVanillaReach checks the box the water search
// walks: within five blocks on the flat and one above or below, so a pond
// one storey down is found and one three up is not.
func TestLookForWaterKeepsToTheVanillaReach(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, stone)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))

	w.set(2, 63, 0, water) // one below the feet
	if _, ok := lookForWater(w, cow, panicWaterRange); !ok {
		t.Error("water one block below the feet was not found, want the reach's one storey")
	}
	w.set(2, 63, 0, stone)

	w.set(2, 67, 0, water) // three above the feet
	if cell, ok := lookForWater(w, cow, panicWaterRange); ok {
		t.Errorf("water three blocks above the feet found at %v, want the reach to stop one up", cell)
	}
}

// TestLookForWaterWalksInManhattanOrder checks the order the search walks:
// (2,64,0) is nearer by manhattan distance than (1,65,1) though the second
// sits on a nearer ring, and the nearer cell is the one handed back.
func TestLookForWaterWalksInManhattanOrder(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, stone)
	w.set(2, 64, 0, water)
	w.set(1, 65, 1, water)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))

	if cell, ok := lookForWater(w, cow, panicWaterRange); !ok || cell != (Waypoint{2, 64, 0}) {
		t.Errorf("lookForWater = %v, %v, want the manhattan-nearest (2,64,0)", cell, ok)
	}
}

// TestLookForWaterBreaksTiesLikeWithinManhattan checks the order equal
// manhattan distances are taken: BlockPos.withinManhattan walks x, then y,
// ascending and +z before the mirrored −z, so the +z pond wins the tie.
func TestLookForWaterBreaksTiesLikeWithinManhattan(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, stone)
	w.set(0, 64, -1, water)
	w.set(0, 64, 1, water)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))

	if cell, ok := lookForWater(w, cow, panicWaterRange); !ok || cell != (Waypoint{0, 64, 1}) {
		t.Errorf("lookForWater = %v, %v, want the tie's +z winner (0,64,1)", cell, ok)
	}
}

// TestAPanicNeedsAPanicCauseEvenWhenBurning checks the order vanilla's
// canUse asks: shouldPanic comes first, so a burning mob whose last hurt is
// no panic cause — a fall — looks for no water and starts nothing.
func TestAPanicNeedsAPanicCauseEvenWhenBurning(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, stone)
	w.set(2, 64, 0, water)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.FireTicks = fireTicksFire
	g := &panicGoal{m: cow}

	cow.noteLastDamage(rules.DamageTypeFall)
	if g.CanStart(&tickView{w: w}) {
		t.Error("a burning cow whose last hurt was a fall started the panic, want shouldPanic to gate it")
	}
	cow.noteLastDamage(rules.DamageTypePlayerAttack)
	if !g.CanStart(&tickView{w: w}) {
		t.Error("a burning cow hurt by a swing did not start the panic")
	}
}

// TestAnAnimalsWalkTargetValueFollowsTheLight checks the fall-through
// vanilla's Animal.getWalkTargetValue gives a non-grass cell: the light
// cost of the cell, not a flat nought, so a dark cell scores below an open
// one while grass stays worth ten.
func TestAnAnimalsWalkTargetValueFollowsTheLight(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	grass := defaultState(t, "minecraft:grass_block")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cell := Waypoint{0, 64, 0}

	// Raw light nought gives the magic value nought, a cost of -0.5.
	if got, want := cow.walkTargetValue(w, cell), -0.5; !closeEnough(got, want) {
		t.Errorf("walk-target value in the dark = %v, want the light cost %v", got, want)
	}
	// Raw light fifteen gives the magic value one, a cost of 0.5.
	w.block = 15
	if got, want := cow.walkTargetValue(w, cell), 0.5; !closeEnough(got, want) {
		t.Errorf("walk-target value in the light = %v, want the light cost %v", got, want)
	}
	// Grass over the cell stays worth the override's ten.
	w.set(0, 63, 0, grass)
	if got := cow.walkTargetValue(w, cell); got != 10 {
		t.Errorf("walk-target value over grass = %v, want 10", got)
	}
}

// TestAMooshroomScoresMyceliumAsTen checks MushroomCow.getWalkTargetValue:
// mycelium under the cell is worth ten, so a mooshroom is not drawn off its
// own ground toward the grass it would otherwise prefer.
func TestAMooshroomScoresMyceliumAsTen(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	mycelium := defaultState(t, "minecraft:mycelium")
	grass := defaultState(t, "minecraft:grass_block")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	mooshroom := aimless(NewMob(TypeMooshroom, 1, 0.5, 64, 0.5, 1))
	cell := Waypoint{0, 64, 0}

	if got := mooshroom.walkTargetValue(w, cell); got >= 10 {
		t.Errorf("walk-target value over bare stone = %v, want the light cost below ten", got)
	}
	w.set(0, 63, 0, mycelium)
	if got := mooshroom.walkTargetValue(w, cell); got != 10 {
		t.Errorf("walk-target value over mycelium = %v, want 10", got)
	}
	// MushroomCow's override replaces Animal's grass ten outright: over
	// grass the mooshroom still scores the light cost.
	w.set(0, 63, 0, grass)
	if got := mooshroom.walkTargetValue(w, cell); got >= 10 {
		t.Errorf("walk-target value over grass = %v, want the light cost below ten", got)
	}
}

// TestAFallIsRecordedAsTheLastHurt checks that a landing that hurts keeps
// its damage type, so it can spend a panic's freshness: a fall is no panic
// cause, so it stops a re-panic.
// vanilla: LivingEntity.hurtServer
func TestAFallIsRecordedAsTheLastHurt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if cow.Health >= cow.MaxHealth {
		t.Fatal("the fall did not hurt the cow")
	}
	if got := cow.lastDamageSource(); got != rules.DamageTypeFall {
		t.Errorf("the last hurt after a landing = %q, want the fall's own", got)
	}
}

// TestALandingBlocksHurtLandsAndIsRecorded checks the block's own hurt on a
// mob: the health comes off, the type is kept for the panic, the block shoves
// the mob away from where it landed, and the half-second window refuses a
// second hurt inside it.
func TestALandingBlocksHurtLandsAndIsRecorded(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	if !cow.HurtByFall(4, rules.DamageTypeFallingAnvil, 0.5, 2.5) {
		t.Fatal("the landing's hurt did not land")
	}
	if !closeEnough(cow.Health, 6) {
		t.Errorf("health = %v, want 6 after four off ten", cow.Health)
	}
	if got := cow.lastDamageSource(); got != rules.DamageTypeFallingAnvil {
		t.Errorf("last hurt = %q, want %q", got, rules.DamageTypeFallingAnvil)
	}
	if !cow.Impulse || cow.VZ >= 0 {
		t.Errorf("the shove = (%v, %v, %v), want the mob pushed away from the block", cow.VX, cow.VY, cow.VZ)
	}
	if cow.HurtByFall(4, rules.DamageTypeFallingAnvil, 0.5, 2.5) {
		t.Error("a second landing hurt landed inside the invulnerability window")
	}
}

// TestALandingKnockbackHalvesAndLiftsLikeVanilla checks the shape of the
// landing's shove on the bodies vanilla's knockback treats differently: a
// body already moving keeps half its velocity under the push and, being off
// the ground, keeps its own vertical speed, where a body on the ground is
// lifted by the capped four tenths; and a body dead under the block's centre
// gets the small random direction rather than no push at all.
func TestALandingKnockbackHalvesAndLiftsLikeVanilla(t *testing.T) {
	// Moving east, in the air, with the block to the west: the old velocity
	// halved under the push, the fall's own vertical speed left alone.
	air := NewMob(TypeCow, 1, 5.5, 70, 0.5, 1)
	air.OnGround = false
	air.VX, air.VY = 0.6, -0.5
	if !air.HurtByFall(4, rules.DamageTypeFallingBlock, 3.5, 0.5) {
		t.Fatal("the landing's hurt did not land")
	}
	if !closeEnough(air.VX, 0.3+knockbackStrength) || air.VZ != 0 {
		t.Errorf("the shove = (%v, %v), want the old velocity halved under the push (%v, 0)",
			air.VX, air.VZ, 0.3+knockbackStrength)
	}
	if !closeEnough(air.VY, -0.5) {
		t.Errorf("an airborne body was lifted to %v, want its own fall kept", air.VY)
	}

	// A grounded body dead under the centre: the random direction nudges it
	// sideways and the jump is the capped four tenths.
	under := NewMob(TypeCow, 2, 5.5, 64, 0.5, 2)
	under.OnGround = true
	if !under.HurtByFall(4, rules.DamageTypeFallingBlock, 5.5, 0.5) {
		t.Fatal("the landing's hurt did not land")
	}
	if math.Hypot(under.VX, under.VZ) < 0.01 {
		t.Errorf("a body under the centre got no push: (%v, %v)", under.VX, under.VZ)
	}
	if !closeEnough(under.VY, knockbackStrength) {
		t.Errorf("a grounded body was lifted to %v, want %v", under.VY, knockbackStrength)
	}
}

// TestASoftenedLandingTakesOnlyItsExcess checks a gravity block's blow that
// lands inside a mob's window: the excess alone reaches the health, the
// shove of a whole blow stays back, and the landing reports no whole hurt
// for the caller to tell of.
func TestASoftenedLandingTakesOnlyItsExcess(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.OnGround = true
	cow.HurtByBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false)

	beforeVX, beforeVY := cow.VX, cow.VY
	if cow.HurtByFall(4, rules.DamageTypeFallingAnvil, 3.5, 0.5) {
		t.Error("a window-softened landing reported a whole hurt, want none")
	}
	if cow.VX != beforeVX || cow.VY != beforeVY {
		t.Errorf("a window-softened landing shoved to (%v, %v), want the velocity left at (%v, %v)",
			cow.VX, cow.VY, beforeVX, beforeVY)
	}
	if want := 10.0 - 1 - 3; !closeEnough(cow.Health, want) {
		t.Errorf("the excess of three left %v health, want the %v", cow.Health, want)
	}
	if got := cow.lastDamageSource(); got != rules.DamageTypeFallingAnvil {
		t.Errorf("the softened landing recorded %q, want the anvil's own", got)
	}
}

// TestAnArrowsBlowKnocksLikeVanilla checks the arrow's shove runs the same
// halves-and-lift shape as the landing's, along the shaft's own flight: a
// body in the air keeps half its velocity under the push, thrown the way the
// arrow flew, and its own vertical speed.
func TestAnArrowsBlowKnocksLikeVanilla(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 70, 0.5, 1)
	cow.OnGround = false
	cow.VX, cow.VY = 0.6, -0.5
	// The shaft flew west (-X), so the shove runs the same way.
	if landed, _ := cow.HurtByArrow(2, -2, 0, 0, 7, true, false); !landed {
		t.Fatal("the arrow's hurt did not land")
	}
	if !closeEnough(cow.VX, 0.3-knockbackStrength) || cow.VZ != 0 {
		t.Errorf("the shove = (%v, %v), want the old velocity halved under the push (%v, 0)",
			cow.VX, cow.VZ, 0.3-knockbackStrength)
	}
	if !closeEnough(cow.VY, -0.5) {
		t.Errorf("an airborne body was lifted to %v, want its own fall kept", cow.VY)
	}
}

// TestAPunchShotAddsItsPushToAMob checks the Punch enchantment's second
// shove on a mob: on top of the whole hit's default the shot adds its 0.6 a
// level along the flight, and a tenth upward, and a window-softened hit
// keeps that extra while the default stands back.
func TestAPunchShotAddsItsPushToAMob(t *testing.T) {
	punch := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	punch.OnGround = true
	// The shaft flew west (-X) with Punch II's knockback of two.
	if landed, _ := punch.HurtByArrow(2, -2, 0, 2, 7, true, false); !landed {
		t.Fatal("the punch shot did not land")
	}
	if want := -knockbackStrength - 1.2; !closeEnough(punch.VX, want) {
		t.Errorf("the punch shot shoved at %v, want the default and the extra's %v", punch.VX, want)
	}
	if want := knockbackStrength + 0.1; !closeEnough(punch.VY, want) {
		t.Errorf("the punch shot lifted at %v, want the default and the extra's %v", punch.VY, want)
	}

	// A softened hit inside the window keeps the extra alone.
	soft := NewMob(TypeZombie, 2, 5.5, 64, 0.5, 1)
	soft.OnGround = true
	soft.HurtByBlast(10, 0, 0, 0, rules.DamageTypeExplosion, false)
	before := soft.VX
	if landed, softened := soft.HurtByArrow(12, -2, 0, 2, 7, true, false); !landed || !softened {
		t.Fatalf("the larger punch shot inside the window = landed %v softened %v, want both", landed, softened)
	}
	if want := before - 1.2; !closeEnough(soft.VX, want) {
		t.Errorf("the softened punch shot shoved at %v, want the extra's %v alone", soft.VX, want)
	}
}

// TestAnInvulnerableMobTurnsAsideOrdinaryHarms checks the invulnerability
// flag's gate on the hurt paths: a survival arrow and a mob's own blow leave
// the body whole and unmoved, a blast leaves it whole but still shoves it,
// while a creative shot lands and a bypassing world type reaches through.
// vanilla: LivingEntity.isInvulnerableTo, LivingEntity.hurtServer
func TestAnInvulnerableMobTurnsAsideOrdinaryHarms(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.Invulnerable = true
	beforeVX, beforeVY, beforeVZ := cow.VX, cow.VY, cow.VZ

	if landed, _ := cow.HurtByArrow(2, -2, 0, 0, 7, true, false); landed {
		t.Error("a survival arrow landed on an invulnerable cow")
	}
	if landed := cow.HurtByMob(2, NewMob(TypeZombie, 2, 0.5, 64, 0.5, 2)); landed {
		t.Error("a mob's blow landed on an invulnerable cow")
	}
	if cow.VX != beforeVX || cow.VY != beforeVY || cow.VZ != beforeVZ {
		t.Errorf("a refused arrow or blow shoved the cow to (%v, %v, %v), want (%v, %v, %v)",
			cow.VX, cow.VY, cow.VZ, beforeVX, beforeVY, beforeVZ)
	}
	// The push lands whatever the hurt says, so a refused hurt still
	// shoves.
	// vanilla: ServerExplosion.hurtEntities
	cow.HurtByBlast(2, -1, 0, 0, rules.DamageTypeExplosion, false)
	if cow.Health != 10 {
		t.Errorf("an invulnerable cow lost health to ordinary harms: %v", cow.Health)
	}
	if cow.VX != beforeVX-1 || cow.VY != beforeVY || cow.VZ != beforeVZ {
		t.Errorf("the blast shoved the cow to (%v, %v, %v), want (%v, %v, %v)",
			cow.VX, cow.VY, cow.VZ, beforeVX-1, beforeVY, beforeVZ)
	}

	if landed, _ := cow.HurtByArrow(2, 0, 0, 0, 7, true, true); !landed {
		t.Error("a creative arrow was turned aside by the flag")
	}
	if cow.Health >= 10 {
		t.Errorf("a creative arrow left the cow at %v, want it hurt", cow.Health)
	}

	fresh := NewMob(TypeCow, 3, 0.5, 64, 0.5, 1)
	fresh.Invulnerable = true
	if landed, _ := fresh.HurtByWorld(2, rules.DamageTypeOutOfWorld); !landed {
		t.Error("out_of_world was turned aside by the flag")
	}
}

// TestAPanicWithNowhereToFleeDoesNotStart checks that the ten tries run in
// canUse: a hurt animal with no walkable ground within reach starts no
// panic, so a failed roll takes no legs from the stroll.
func TestAPanicWithNowhereToFleeDoesNotStart(t *testing.T) {
	w := newTestWorld() // bare air: nothing to stand on anywhere
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	if !cow.TakeDamage(3, Player{X: 0.5, Y: 64, Z: 1.5}) {
		t.Fatal("the hit did not land")
	}
	cow.Tick(w, nil)
	if panicking(cow) {
		t.Error("a hurt cow with nowhere walkable started the panic, want a failed roll to start nothing")
	}
}

// TestAFleeNeverPicksAHarmfulCell checks that DefaultRandomPos rejects the
// cells GoalUtils.hasMalus names: with every walkable cell of a fire-covered
// platform harmful, no seed's roll may hand one back.
func TestAFleeNeverPicksAHarmfulCell(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, stone)
	for x := int32(-6); x <= 6; x++ {
		for z := int32(-6); z <= 6; z++ {
			w.set(x, 64, z, fire)
		}
	}
	for seed := int64(1); seed <= 100; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		cell, ok := defaultRandomPos(w, cow, panicFleeSpreadXZ, panicFleeSpreadY)
		if ok && rules.IsFire(w.BlockAt(cell.X, cell.Y, cell.Z)) {
			t.Fatalf("seed %d picked the fire cell %v to flee to", seed, cell)
		}
	}
}

// panicking reports whether the panic goal holds the mob's legs.
func panicking(m *Mob) bool { return panicOf(m) != nil }

// panicOf hands back the panic goal holding the mob's legs, or nil.
func panicOf(m *Mob) *panicGoal {
	for _, g := range m.ai.running {
		if p, ok := g.(*panicGoal); ok {
			return p
		}
	}
	return nil
}

// panicLegs walks a burning cow through its burns and counts the flee legs
// the panic takes, a leg being one choice of somewhere to flee to. Each
// burn's bites keep the hurt fresh to the burn's end and the forty ticks
// past it, so the legs come one after another the while; a burn spent, the
// flame is set under the cow's feet again for the next. The health is held
// whole, the legs being the count's business and not the bites'. What comes
// back is the count and, for each leg, the angle the flee ran along.
func panicLegs(t *testing.T, cow *Mob, w testWorld, legsWanted int) (legs int, angles []float64) {
	t.Helper()
	lastX, lastZ := 0.0, 0.0
	for cycle := 0; cycle < 3 && legs < legsWanted; cycle++ {
		foot := cow.cell()
		w.set(foot.X, foot.Y, foot.Z, defaultState(t, "minecraft:fire"))
		for tick := 0; tick < 300 && legs < legsWanted; tick++ {
			cow.Health = cow.MaxHealth
			wasX, wasZ := cow.X, cow.Z
			cow.Tick(w, nil)
			if g := panicOf(cow); g != nil && (g.fleeX != lastX || g.fleeZ != lastZ) {
				legs++
				angles = append(angles, math.Atan2(g.fleeZ-wasZ, g.fleeX-wasX))
				lastX, lastZ = g.fleeX, g.fleeZ
			}
			if cow.Dead {
				t.Fatal("the burning cow died in its legs")
			}
			if cow.FireTicks == 0 && cow.lastDamageSource() == 0 {
				break
			}
		}
	}
	return legs, angles
}

// TestAKnockbackShovesAMobAwayFromTheHit checks the direction the hit
// throws a mob and its invulnerability window: the same four tenths of a
// block a tick along the line away from the hit and upward, a larger hit
// inside the window landing only its excess above the last and shoving
// nothing, and a hit from straight overhead taking vanilla's random nudge.
func TestAKnockbackShovesAMobAwayFromTheHit(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	cow.OnGround = true

	// The hit comes from the east (+X); away is west (-X).
	if !cow.TakeDamage(1, Player{X: 7.5, Y: 64, Z: 0.5}) {
		t.Fatal("the first hit did not land")
	}
	if !closeEnough(cow.VX, -knockbackStrength) || !closeEnough(cow.VY, knockbackStrength) || cow.VZ != 0 {
		t.Errorf("knockback velocity = (%v, %v, %v), want (%v, %v, 0)",
			cow.VX, cow.VY, cow.VZ, -knockbackStrength, knockbackStrength)
	}

	// A larger hit inside the window lands only its excess above the last
	// one, and leaves the velocity of the first alone.
	before := cow.VX
	if !cow.TakeDamage(5, Player{X: 5.6, Y: 64, Z: 0.5}) {
		t.Error("a hit of five with four of excess over the last hurt was absorbed outright")
	}
	if cow.VX != before {
		t.Errorf("a window-softened hit changed the velocity: %v, want %v", cow.VX, before)
	}
	if cow.Health != cow.MaxHealth-1-4 {
		t.Errorf("a hit of five over the last hurt's one left %v health, want the excess's %v",
			cow.Health, cow.MaxHealth-1-4)
	}

	// From straight overhead there is no away along the ground, so vanilla's
	// knockback draws a direction: a full four tenths' horizontal push, and
	// the jump out of it. A fresh bird, so the window has nothing to say and
	// the first hit nothing to leave behind.
	hen := NewMob(TypeChicken, 2, 5.5, 64, 0.5, 7)
	hen.OnGround = true
	if !hen.TakeDamage(1, Player{X: hen.X, Y: hen.Y + 2, Z: hen.Z}) {
		t.Fatal("the overhead hit did not land")
	}
	if got := math.Hypot(hen.VX, hen.VZ); !closeEnough(got, knockbackStrength) {
		t.Errorf("an overhead hit shoved along the ground at %v, want the random direction's %v", got, knockbackStrength)
	}
	if hen.VY != knockbackStrength {
		t.Errorf("an overhead hit jumped at %v, want the vanilla push up", hen.VY)
	}
}

// TestAKnockInTheAirLiftsNoBody is vanilla's knockback: a hit a body takes
// in mid-air halves its standing velocity into its push and adds no lift of
// its own, so the fall goes on — only a body on the ground is thrown into
// its arc.
// vanilla: LivingEntity.knockback
func TestAKnockInTheAirLiftsNoBody(t *testing.T) {
	falling := NewMob(TypeCow, 1, 5.5, 70, 0.5, 5)
	falling.VX = 0.3
	falling.VY = -0.2

	if !falling.TakeDamage(1, Player{X: 7.5, Y: 70, Z: 0.5}) {
		t.Fatal("the mid-air hit did not land")
	}
	if falling.VY != -0.2 {
		t.Errorf("a mid-air hit lifted the body to %v, want the fall kept at %v", falling.VY, -0.2)
	}
	if falling.OnGround {
		t.Error("a mid-air hit set the grounded bit")
	}
	if want := 0.3/2 - 0.4; !closeEnough(falling.VX, want) {
		t.Errorf("a mid-air hit shoved at %v, want the halved and pushed %v", falling.VX, want)
	}
}

// TestABlastsWindowHoldsTheRawBlow checks the window a blast opens holds
// the raw blow before armour: a melee blow of the blast's own size is
// refused, and a larger one lands the armour of its excess alone, not the
// difference of two armoured blows.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func TestABlastsWindowHoldsTheRawBlow(t *testing.T) {
	equal := NewMob(TypeZombie, 1, 5.5, 64, 0.5, 1)
	equal.OnGround = true
	equal.HurtByBlast(10, 0, 0, 0, rules.DamageTypeExplosion, false)
	// Armour is evaluated in float32, like CombatRules.getDamageAfterAbsorb.
	if want := 20 - 10*0.984; math.Abs(equal.Health-want) > 1e-6 {
		t.Fatalf("a blast of ten left %v health, want the armoured %v", equal.Health, want)
	}
	if equal.TakeDamage(10, Player{X: 7.5, Y: 64, Z: 0.5}) {
		t.Error("a melee blow of the blast's own size landed inside its window, want it refused")
	}
	if want := 20 - 10*0.984; math.Abs(equal.Health-want) > 1e-6 {
		t.Errorf("the refused blow took health to %v, want the blast's %v", equal.Health, want)
	}

	// The armour formula bends below four points of damage, so the excess
	// is armoured on its own: three points become 2.94, and a melee of
	// five inside the window lands the excess two as 1.92, not the 1.98 the
	// difference of two armoured blows would.
	excess := NewMob(TypeZombie, 2, 5.5, 64, 0.5, 7)
	excess.OnGround = true
	excess.HurtByBlast(3, 0, 0, 0, rules.DamageTypeExplosion, false)
	if !excess.TakeDamage(5, Player{X: 7.5, Y: 64, Z: 0.5}) {
		t.Fatal("the larger blow inside the window did not land")
	}
	if want := 20 - 3*0.98 - 2*0.96; math.Abs(excess.Health-want) > 1e-6 {
		t.Errorf("the excess of two left %v health, want the armoured excess's %v", excess.Health, want)
	}
}

// TestAWindowSoftenedBlowKeepsEnchantedKnockback checks the shove a
// window-softened enchanted blow keeps: vanilla's Player.attack still runs
// causeExtraKnockback for a blow that landed only its excess, while the
// default shove the window's own gate holds back — so the shove is the
// enchantment's extra alone, not that half-shoved into the default's.
func TestAWindowSoftenedBlowKeepsEnchantedKnockback(t *testing.T) {
	enchanted := NewMob(TypeZombie, 1, 5.5, 64, 0.5, 1)
	enchanted.OnGround = true
	enchanted.HurtByBlast(10, 0, 0, 0, rules.DamageTypeExplosion, false)
	// The attacker stands east and looks west, so the extra shove runs -X.
	if landed, softened := enchanted.TakeDamageExtra(12, Player{X: 7.5, Y: 64, Z: 0.5, Yaw: 90}, 1); !landed || !softened {
		t.Fatalf("the larger enchanted blow inside the window = landed %v softened %v, want both", landed, softened)
	}
	if !closeEnough(enchanted.VX, -1) {
		t.Errorf("the softened enchanted blow shoved at %v, want the enchantment's -1", enchanted.VX)
	}

	// A plain blow softened the same way deals no shove at all.
	plain := NewMob(TypeZombie, 2, 5.5, 64, 0.5, 2)
	plain.OnGround = true
	plain.HurtByBlast(10, 0, 0, 0, rules.DamageTypeExplosion, false)
	if landed, softened := plain.TakeDamageExtra(12, Player{X: 7.5, Y: 64, Z: 0.5}, 0); !landed || !softened {
		t.Fatalf("the larger plain blow inside the window = landed %v softened %v, want both", landed, softened)
	}
	if plain.VX != 0 {
		t.Errorf("the softened plain blow shoved at %v, want none", plain.VX)
	}
}

// TestAnEnchantedBlowHalvesAMovingBodysVelocityTwice checks vanilla's two
// knockback calls: the default shove and the enchantment's extra each halve
// the standing velocity, so a body already moving keeps a quarter of it.
func TestAnEnchantedBlowHalvesAMovingBodysVelocityTwice(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	cow.OnGround = true
	cow.VX = 1.0

	// The attacker stands east and looks west, so both calls shove west
	// (-X): the hurt's own 0.4 along the away line and the extra 0.5 along
	// the facing, each halving what the last left.
	if landed, _ := cow.TakeDamageExtra(1, Player{X: 7.5, Y: 64, Z: 0.5, Yaw: 90}, 0.5); !landed {
		t.Fatal("the enchanted blow did not land")
	}
	if want := 1.0/4 - 0.4/2 - 0.5; !closeEnough(cow.VX, want) {
		t.Errorf("the enchanted blow left VX %v, want the twice-halved %v", cow.VX, want)
	}
}

// TestAnEnchantedBlowsLiftHalvesLikeTheTwoCalls checks the vertical share of
// the same two calls: each lift halves the last and caps at four tenths, so
// the default 0.4 lifts the resting -0.0784 to 0.3608 and the extra 0.1 to
// 0.2804.
// vanilla: LivingEntity.knockback, Player.attack
func TestAnEnchantedBlowsLiftHalvesLikeTheTwoCalls(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	cow.OnGround = true
	cow.VY = -0.0784 // the resting fall a grounded body carries

	if landed, _ := cow.TakeDamageExtra(1, Player{X: 7.5, Y: 64, Z: 0.5}, 0.1); !landed {
		t.Fatal("the enchanted blow did not land")
	}
	if want := 0.2804; !closeEnough(cow.VY, want) {
		t.Errorf("the enchanted blow lifted the body to %v, want vanilla's %v", cow.VY, want)
	}
}

// TestAThrownBlowLandsWhateverItsDamage checks the hurt a thrown
// projectile's blow runs: the egg's nought lands all the same, opening the
// half-second window and keeping the thrower for the retaliation, the
// snowball's three off a zombie go through the armour, and the knockback
// runs along the flight the projectile came on.
func TestAThrownBlowLandsWhateverItsDamage(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	cow.OnGround = true

	// The egg's blow: no damage in it, and the hurt lands all the same.
	if landed, _ := cow.HurtByThrown(0, 1.4, 0, 7, true, false); !landed {
		t.Fatal("the no-damage blow did not land")
	}
	if cow.Health != cow.MaxHealth {
		t.Errorf("the no-damage blow took health down to %v, want it left whole", cow.Health)
	}
	if cow.InvulnTicks != invulnerableTime {
		t.Errorf("the hurt left an invulnerability window of %v, want invulnerableTime's %v", cow.InvulnTicks, invulnerableTime)
	}
	// The flight came on east (+X); the shove runs the same way, the old
	// velocity halved away, with the grounded body's jump out of it.
	if !closeEnough(cow.VX, knockbackStrength) || !closeEnough(cow.VY, knockbackStrength) || cow.VZ != 0 {
		t.Errorf("knockback velocity = (%v, %v, %v), want (%v, %v, 0)",
			cow.VX, cow.VY, cow.VZ, knockbackStrength, knockbackStrength)
	}
	if !cow.hurtByPlayer {
		t.Error("the blow of a player's throw left no player behind it")
	}

	// A second no-damage blow inside the window lands nothing — a nought
	// has no excess above the last hurt.
	if landed, _ := cow.HurtByThrown(0, -1.4, 0, 7, true, false); landed {
		t.Error("a second blow inside the invulnerability window landed")
	}
	if !closeEnough(cow.VX, knockbackStrength) {
		t.Errorf("a refused blow changed the velocity: %v, want %v", cow.VX, knockbackStrength)
	}

	// The snowball's blow off a blaze is three points through the armour;
	// the zombie's own two points turn their share of it away, the share
	// the vanilla armour formula leaves of three points' reach.
	zombie := NewMob(TypeZombie, 2, 5.5, 64, 0.5, 2)
	zombie.OnGround = true
	if landed, _ := zombie.HurtByThrown(3, 0, 1.4, 7, true, false); !landed {
		t.Fatal("the three-point blow did not land")
	}
	if want := 20 - 3*0.98; math.Abs(zombie.Health-want) > 1e-6 {
		t.Errorf("health after three points = %v, want the armoured %v", zombie.Health, want)
	}
	// The flight came on north (+Z); the shove runs the same way.
	if !closeEnough(zombie.VZ, knockbackStrength) || zombie.VX != 0 {
		t.Errorf("knockback velocity = (%v, %v), want (0, %v)", zombie.VX, zombie.VZ, knockbackStrength)
	}
}

// TestAReducedThrownBlowIsSoftened checks the second return of a thrown
// blow that lands inside a window: its excess alone reaches the health and
// the shove of a whole hit stays back, so the server can tell a softened
// landing from a whole one.
func TestAReducedThrownBlowIsSoftened(t *testing.T) {
	cow := NewMob(TypeCow, 1, 5.5, 64, 0.5, 1)
	cow.OnGround = true
	cow.HurtByBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false)

	before := cow.VX
	landed, softened := cow.HurtByThrown(3, 1.4, 0, 7, true, false)
	if !landed || !softened {
		t.Fatalf("a thrown blow of three over the last hurt's one = landed %v softened %v, want both", landed, softened)
	}
	if cow.VX != before {
		t.Errorf("a softened thrown blow shoved at %v, want the velocity left at %v", cow.VX, before)
	}
	if want := 10.0 - 1 - 2; !closeEnough(cow.Health, want) {
		t.Errorf("the excess of two left %v health, want the %v", cow.Health, want)
	}
}

// TestAZombiesArmourTurnsAHitAway checks the natural armour the kinds wear:
// the zombie's two points turn what they can of a blow away, the share the
// vanilla armour formula leaves, while an unarmoured kind takes the whole
// amount.
func TestAZombiesArmourTurnsAHitAway(t *testing.T) {
	attacker := Player{X: 2.5, Y: 64, Z: 0.5}

	zombie := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
	if landed, _ := zombie.TakeDamageExtra(9, attacker, 0); !landed {
		t.Fatal("the hit did not land")
	}
	if want := 20 - 9*0.984; math.Abs(zombie.Health-want) > 1e-5 {
		t.Errorf("health after nine points of damage = %v, want the armoured %v", zombie.Health, want)
	}

	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	if landed, _ := cow.TakeDamageExtra(9, attacker, 0); !landed {
		t.Fatal("the hit did not land")
	}
	if !closeEnough(cow.Health, 1) {
		t.Errorf("health after nine points of damage = %v, want the whole nine off the ten", cow.Health)
	}
}

// TestAFireBlockBitesAZombieThroughItsArmour stands a zombie in a fire and
// counts the bites: the two points of armour the zombie wears turn each of
// the fire block's one-point bites into 0.94 — vanilla's 20 → 19.06 →
// 18.12 — paced ten ticks apart by the invulnerability window.
func TestAFireBlockBitesAZombieThroughItsArmour(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)
	w.set(0, 64, 0, fire)

	z := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	bites := 0
	for range 12 {
		for _, ev := range z.Tick(w, nil) {
			if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeInFire {
				bites++
			}
		}
	}
	if bites != 2 {
		t.Fatalf("twelve ticks in fire bit the zombie %d times, want the window's two", bites)
	}
	if want := 20 - 2*0.94; math.Abs(z.Health-want) > 1e-5 {
		t.Errorf("health after two bites = %v, want the armoured %v", z.Health, want)
	}
}

// TestAKillingFireBiteIsStillToldOf checks the bite that ends the body: the
// damage event goes out ahead of the death, so the killing bite is told of
// like the rest.
// vanilla: LivingEntity.hurtServer
func TestAKillingFireBiteIsStillToldOf(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	fire := defaultState(t, "minecraft:fire")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)
	w.set(0, 64, 0, fire)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.Health = 1
	events := cow.Tick(w, nil)
	if len(events) != 2 || events[0].Kind != MobBurned || events[0].DamageType != rules.DamageTypeInFire {
		t.Fatalf("the killing bite's events = %+v, want the fire's bite first", events)
	}
	if events[1].Kind != MobDied {
		t.Errorf("the death came %v after the bite, want the bite told of first", events[1].Kind)
	}
}

// TestAFourBlockFallCostsAHeart checks the floor of the vanilla formula at
// the whole-block boundary: a fall of exactly four blocks is one past the
// free three, and the floor of it takes one point off the cow.
func TestAFourBlockFallCostsAHeart(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.0, 0.5, 1))
	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if !cow.OnGround {
		t.Fatal("the cow never came to rest on the floor")
	}
	if !closeEnough(cow.Health, 9) {
		t.Errorf("health after a four-block fall = %v, want the nine of ten the floor charges", cow.Health)
	}
}

// TestAFallIntoWaterIsForgiven checks the soft landing: a pool the feet end
// in writes the fall off, however far it was. The move that carries a body
// falling fast from air onto a pool's floor leaves the feet in the water —
// the state vanilla's checkFallDamage refreshes after the move, its
// LivingEntity override re-reading the fluid on the spot. The control is
// the same crossing onto the stone beside the pool, which the stone's own
// landing charges for.
func TestAFallIntoWaterIsForgiven(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	// A pool two deep over stone: water at 63 and 64, the floor's stone at
	// 62 — a fall onto it ends on the stone's top face at 63, the feet in
	// the water cell the move arrived through.
	floor(w, -2, 0, 2, 4, 62, stone)
	for x := -2; x <= 2; x++ {
		for z := 0; z <= 2; z++ {
			w.set(int32(x), 63, int32(z), water)
			w.set(int32(x), 64, int32(z), water)
		}
	}

	// A gentle drop into the pool: the body slows in the water it falls
	// through and rests on the floor unhurt.
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if !cow.OnGround {
		t.Fatal("the cow never came to rest in the pool")
	}
	if !closeEnough(cow.Health, 10) {
		t.Errorf("health after a fall into the pool = %v, want the water's whole ten", cow.Health)
	}
	if cow.FallDistance != 0 {
		t.Errorf("fall distance after the water's forgiveness = %v, want it written off", cow.FallDistance)
	}

	// The crossing: a body already fallen ten blocks' worth arrives over
	// the pool at better than two blocks a tick, in the air above it, and
	// the one move that lands it crosses the water and ends on the pool's
	// floor. The water the move ends in writes the whole fall off.
	fast := aimless(NewMob(TypeCow, 2, 0.5, 65.4, 0.5, 2))
	fast.FallDistance = 10
	fast.VY = -2.5
	fast.Tick(w, nil)
	if !fast.OnGround {
		t.Fatal("the fast cow never came to rest in the pool")
	}
	if !closeEnough(fast.Y, 63) {
		t.Errorf("the fast cow rested at y = %v, want the pool floor's top face at 63", fast.Y)
	}
	if !closeEnough(fast.Health, 10) {
		t.Errorf("health after the fast crossing = %v, want the water's whole ten", fast.Health)
	}

	// The control: the same crossing onto the stone south of the pool is
	// charged for — twelve and four tenths banked, nine past the free
	// three, leaving the cow on its last point.
	land := aimless(NewMob(TypeCow, 3, 0.5, 65.4, 3.5, 3))
	land.FallDistance = 10
	land.VY = -2.5
	land.Tick(w, nil)
	if !land.OnGround {
		t.Fatal("the control cow never came to rest on the stone")
	}
	if !closeEnough(land.Health, 1) {
		t.Errorf("health after the control crossing = %v, want the charged 1", land.Health)
	}
}

// TestABabyIsTheSmallerBoxItsKindGrowsOutOf checks the measures a baby is
// read from: the box and the eyes of the kind's own BABY_DIMENSIONS while
// the age is below nought, the grown ones the moment it is not — and the
// baby zombie's haste, the half again its modifier multiplies the base up
// to. The kinds that are never babies read the grown box whatever their
// age is poked to.
func TestABabyIsTheSmallerBoxItsKindGrowsOutOf(t *testing.T) {
	hen := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 1)
	if hen.width() != 0.4 || hen.height() != 0.7 || !closeEnough(hen.eyeHeight(), 0.7*0.85) {
		t.Fatalf("the grown chicken's measures are not the grown ones: %v %v %v",
			hen.width(), hen.height(), hen.eyeHeight())
	}
	hen.SetBaby(true)
	if !hen.IsBaby() {
		t.Fatal("a hen set baby is no baby")
	}
	if !closeEnough(hen.width(), 0.3) || !closeEnough(hen.height(), 0.4) || !closeEnough(hen.eyeHeight(), 0.28125) {
		t.Errorf("the chick's measures = %v %v %v, want the 0.3 by 0.4 box with its eyes 0.28125 up",
			hen.width(), hen.height(), hen.eyeHeight())
	}
	hen.SetBaby(false)
	if hen.IsBaby() || hen.width() != 0.4 || !closeEnough(hen.eyeHeight(), 0.7*0.85) {
		t.Error("a grown hen did not come back to the grown measures")
	}

	zombie := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 2)
	if !closeEnough(zombie.pace(), float64(float32(0.23))) {
		t.Errorf("the grown zombie's pace = %v, want the vanilla 0.23", zombie.pace())
	}
	zombie.SetBaby(true)
	if !closeEnough(zombie.width(), 0.49) || !closeEnough(zombie.height(), 0.98) || !closeEnough(zombie.eyeHeight(), 0.775) {
		t.Errorf("the baby zombie's box = %v by %v with its eyes %v up, want the vanilla baby's",
			zombie.width(), zombie.height(), zombie.eyeHeight())
	}
	if !closeEnough(zombie.pace(), float64(float32(0.23))*1.5) {
		t.Errorf("the baby zombie's pace = %v, want the haste's half again 0.345", zombie.pace())
	}

	calf := NewMob(TypeCow, 3, 0.5, 64, 0.5, 3)
	calf.SetBaby(true)
	if !closeEnough(calf.width(), 0.45) || !closeEnough(calf.height(), 0.7) || !closeEnough(calf.eyeHeight(), 0.69) {
		t.Errorf("the calf's box = %v by %v with its eyes %v up, want the vanilla calf's",
			calf.width(), calf.height(), calf.eyeHeight())
	}

	spider := NewMob(TypeSpider, 4, 0.5, 64, 0.5, 4)
	spider.SetBaby(true)
	if spider.IsBaby() {
		t.Error("a spider read as a baby")
	}
	if spider.width() != 1.4 || spider.height() != 0.9 {
		t.Errorf("the spider's box = %v by %v, want the grown one it has no baby of", spider.width(), spider.height())
	}
}

// TestABabyGrowsUpAtTheCrossingOfItsAge checks the growing: the age counts
// up a tick at a time from the minus twenty-four thousand it was born at,
// and the tick it crosses nought the body is grown — the box back
// at the grown measures, and the egg countdown of a hen waiting no longer
// than the growing took.
func TestABabyGrowsUpAtTheCrossingOfItsAge(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)

	hen := aimless(NewMob(TypeChicken, 1, 0.5, 64, 0.5, 1))
	hen.SetBaby(true)
	hen.EggTimer = 1
	events := hen.Tick(w, nil)
	if !hen.IsBaby() {
		t.Fatal("a baby hen did not stay a baby over its tick")
	}
	for _, ev := range events {
		if ev.Kind == MobLaidEgg {
			t.Error("a baby hen laid an egg")
		}
	}

	hen.BabyAge = -2
	hen.Tick(w, nil)
	if !hen.IsBaby() {
		t.Fatal("the hen grew a tick early, want the last tick of the -2 to still be baby")
	}
	hen.Tick(w, nil)
	if hen.IsBaby() {
		t.Fatal("the hen stayed a baby past the crossing of its age")
	}
	if !closeEnough(hen.width(), 0.4) {
		t.Errorf("the grown hen's width = %v, want the grown box back", hen.width())
	}
	hen.EggTimer = 1
	events = hen.Tick(w, nil)
	laid := false
	for _, ev := range events {
		if ev.Kind == MobLaidEgg {
			laid = true
		}
	}
	if !laid {
		t.Error("a grown hen's countdown never laid")
	}
}

// TestABabyZombieStaysABaby checks the zombie's own keeping of the state:
// its baby-ness is the boolean its metadata carries, with no growing
// behind it — the ticks a baby zombie's age is poked along change nothing,
// the body a baby to the end of its days.
func TestABabyZombieStaysABaby(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, stone)

	zombie := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
	zombie.SetBaby(true)
	for range 24001 {
		zombie.Tick(w, nil)
	}
	if !zombie.IsBaby() {
		t.Fatal("a baby zombie grew up, want the baby it stays")
	}
	if !closeEnough(zombie.height(), 0.98) {
		t.Errorf("the baby zombie's height = %v, want the baby box it keeps", zombie.height())
	}
}

// TestABabyDropsByKind checks the gate a baby's death reads against its
// kind: an animal's baby leaves nothing, LivingEntity.shouldDropLoot and
// shouldDropExperience reading !isBaby(), while a monster's baby rolls on —
// Monster overrides both — with Zombie.getBaseExperienceReward scaling a
// baby zombie's reward by 2.5.
func TestABabyDropsByKind(t *testing.T) {
	calf := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	calf.SetBaby(true)
	if drops := calf.RollDrops(rules.LootKiller{}, nil); drops != nil {
		t.Errorf("a baby animal's death rolled %v stacks of loot, want none", len(drops))
	}
	if xp := calf.RollExperience(rules.LootKiller{}); xp != 0 {
		t.Errorf("a baby animal's death rolled %d of experience, want none", xp)
	}

	// The grown the same kind rolls from: the loot of its table and the
	// experience of its range, the gates no-ops where no baby stands.
	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	if xp := cow.RollExperience(rules.LootKiller{}); xp == 0 {
		t.Error("a grown cow's death rolled no experience")
	}

	// A monster's baby rolls its kind's table like an adult, and the
	// zombie's five of base experience become the twelve of 5 * 2.5.
	for _, tc := range []struct {
		name string
		kind MobType
		seed int64
	}{
		{"zombie", TypeZombie, 3},
		{"husk", TypeHusk, 4},
	} {
		baby := NewMob(tc.kind, 5, 0.5, 64, 0.5, tc.seed)
		baby.SetBaby(true)
		dropped := 0
		for range 200 {
			dropped += len(baby.RollDrops(rules.LootKiller{}, nil))
		}
		if dropped == 0 {
			t.Errorf("200 deaths of a baby %s rolled no loot, want its table's drops", tc.name)
		}
		if xp := baby.RollExperience(rules.LootKiller{}); xp != 12 {
			t.Errorf("a baby %s's death rolled %d of experience, want 12", tc.name, xp)
		}
	}

	// Animal-like monster-category kinds: a baby hoglin drops no loot but
	// gives experience; a baby zombie horse gives neither.
	rolled := func(m *Mob) int {
		n := 0
		for range 200 {
			n += len(m.RollDrops(rules.LootKiller{}, nil))
		}
		return n
	}
	grown := NewMob(TypeHoglin, 6, 0.5, 64, 0.5, 6)
	if rolled(grown) == 0 {
		t.Error("200 deaths of a grown hoglin rolled no loot")
	}
	hoglin := NewMob(TypeHoglin, 7, 0.5, 64, 0.5, 7)
	hoglin.SetBaby(true)
	if n := rolled(hoglin); n != 0 {
		t.Errorf("a baby hoglin rolled %d stacks of loot, want none", n)
	}
	if xp := hoglin.RollExperience(rules.LootKiller{}); xp == 0 {
		t.Error("a baby hoglin's death rolled no experience")
	}
	for _, kind := range []MobType{TypeZombieHorse} {
		baby := NewMob(kind, 8, 0.5, 64, 0.5, 8)
		baby.SetBaby(true)
		if n := rolled(baby); n != 0 {
			t.Errorf("a baby %s rolled %d stacks of loot, want none", kind.Name(), n)
		}
		if xp := baby.RollExperience(rules.LootKiller{}); xp != 0 {
			t.Errorf("a baby %s rolled %d of experience, want none", kind.Name(), xp)
		}
	}
}

// TestAFinalizeDecidesThePacksBabies checks the two halves of the babies a
// finalize decides: an animal's pack leaving its first member grown and
// rolling each later one against the five in a hundred, and a zombie's pack
// deciding all of it with the one roll its first finalize makes — the dice's
// second draw left unspent. A group claimed by one sort is none of the
// other's, and a spawn with no group behind it decides the zombie's odds on
// the mob's own dice.
func TestAFinalizeDecidesThePacksBabies(t *testing.T) {
	// A die whose first draw lands under the five in a hundred, and one
	// that does not.
	babySeed, grownSeed := int64(0), int64(0)
	for s := int64(1); babySeed == 0 || grownSeed == 0; s++ {
		f := rand.New(rand.NewSource(s)).Float64()
		if f < 0.05 && babySeed == 0 {
			babySeed = s
		}
		if f >= 0.05 && grownSeed == 0 {
			grownSeed = s
		}
	}

	// The animal's pack: the first grown whatever the dice say, the
	// second the roll's own answer.
	herd := NewSpawnGroup(GoDice{rand.New(rand.NewSource(babySeed))})
	first := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	first.FinalizeSpawn(GoDice{first.Rand}, testBiome("minecraft:plains"), herd, rules.LocalDifficulty{})
	if first.IsBaby() {
		t.Error("the first of an animal's pack was born a baby")
	}
	second := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	second.FinalizeSpawn(GoDice{second.Rand}, testBiome("minecraft:plains"), herd, rules.LocalDifficulty{})
	if !second.IsBaby() {
		t.Error("the second of the pack was not born the baby its dice rolled")
	}

	herd = NewSpawnGroup(GoDice{rand.New(rand.NewSource(grownSeed))})
	NewMob(TypeCow, 3, 0.5, 64, 0.5, 3).FinalizeSpawn(GoDice{rand.New(rand.NewSource(1))}, testBiome("minecraft:plains"), herd, rules.LocalDifficulty{})
	third := NewMob(TypeCow, 4, 0.5, 64, 0.5, 4)
	third.FinalizeSpawn(GoDice{third.Rand}, testBiome("minecraft:plains"), herd, rules.LocalDifficulty{})
	if third.IsBaby() {
		t.Error("the second of the pack was born a baby against dice that rolled grown")
	}

	// The zombie's pack: one roll at the first finalize, every member
	// after it the same answer — the dice's second draw never spent.
	pack := NewSpawnGroup(GoDice{rand.New(rand.NewSource(babySeed))})
	for i := range 4 {
		z := NewMob(TypeZombie, int32(5+i), 0.5, 64, 0.5, int64(5+i))
		z.FinalizeSpawn(GoDice{z.Rand}, testBiome("minecraft:plains"), pack, rules.LocalDifficulty{})
		if !z.IsBaby() {
			t.Errorf("the zombie %d of a pack whose roll made babies came out grown", i)
		}
	}

	pack = NewSpawnGroup(GoDice{rand.New(rand.NewSource(grownSeed))})
	for i := range 4 {
		z := NewMob(TypeZombie, int32(9+i), 0.5, 64, 0.5, int64(9+i))
		z.FinalizeSpawn(GoDice{z.Rand}, testBiome("minecraft:plains"), pack, rules.LocalDifficulty{})
		if z.IsBaby() {
			t.Errorf("the zombie %d of a pack whose roll made grown came out a baby", i)
		}
	}

	// A group a zombie claimed is none of the cow's: the cow stands up
	// grown and the group's size is none of its doing.
	mixed := NewSpawnGroup(GoDice{rand.New(rand.NewSource(babySeed))})
	NewMob(TypeZombie, 13, 0.5, 64, 0.5, 13).FinalizeSpawn(GoDice{rand.New(rand.NewSource(1))}, testBiome("minecraft:plains"), mixed, rules.LocalDifficulty{})
	cow := NewMob(TypeCow, 14, 0.5, 64, 0.5, 14)
	cow.FinalizeSpawn(GoDice{cow.Rand}, testBiome("minecraft:plains"), mixed, rules.LocalDifficulty{})
	if cow.IsBaby() {
		t.Error("a cow finalized into a zombie's pack was born a baby")
	}
	if mixed.size != 0 {
		t.Errorf("a cow finalized into a zombie's pack grew the group's size to %d", mixed.size)
	}

	// A spawn with no pack behind it: the zombie decides on its own dice,
	// the animal grown.
	// Seed 42's fifth draw (after the Mob step and the pickup roll) is below 0.05.
	alone := NewMob(TypeZombie, 15, 0.5, 64, 0.5, 42)
	alone.FinalizeSpawn(GoDice{rand.New(rand.NewSource(42))}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	if !alone.IsBaby() {
		t.Error("a zombie finalized alone with dice that rolled baby came out grown")
	}
	sheep := NewMob(TypeSheep, 16, 0.5, 64, 0.5, babySeed)
	sheep.FinalizeSpawn(GoDice{sheep.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	if sheep.IsBaby() {
		t.Error("a sheep finalized alone was born a baby")
	}
	// The sheep's colour is still the finalize's to roll, group or none.
	if sheep.Colour == "" {
		t.Error("the sheep finalized alone came out with no colour")
	}
}

// TestAPigFollowsAPlayerHoldingACarrot checks the tempting: a pig walks to
// a player holding what pigs go after, and takes no notice of one holding
// anything else.
func TestAPigFollowsAPlayerHoldingACarrot(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	pig := NewMob(TypePig, 1, 0.5, 64, 0.5, 2)
	stoneHand := Player{X: 0.5, Y: 64, Z: 6.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}}
	for range 100 {
		pig.Tick(w, []Player{stoneHand})
	}
	if drifted := math.Hypot(pig.X-0.5, pig.Z-0.5); drifted > 0.5 {
		t.Errorf("the pig drifted %v toward a player holding stone, want it unmoved", drifted)
	}

	carrotHand := Player{X: 0.5, Y: 64, Z: 6.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:carrot"), Count: 1}}
	for range 200 {
		pig.Tick(w, []Player{carrotHand})
	}
	if distance := math.Hypot(pig.X-carrotHand.X, pig.Z-carrotHand.Z); distance > 3 {
		t.Errorf("the pig stands %v from the player holding a carrot, want it come along", distance)
	}
}

// TestAPigFollowsACarrotOnAStickAtPriorityFour checks that a pig is tempted
// by a carrot on a stick and registers its tempt goal at vanilla's 4.
func TestAPigFollowsACarrotOnAStickAtPriorityFour(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	pig := NewMob(TypePig, 1, 0.5, 64, 0.5, 2)
	got := map[string]int{}
	for _, g := range pig.ai.goals {
		switch g.(type) {
		case *breedGoal:
			got["breed"] = g.Priority()
		case *temptGoal:
			got["tempt"] = g.Priority()
		case *followParentGoal:
			got["follow"] = g.Priority()
		}
	}
	if got["breed"] != 3 || got["tempt"] != 4 || got["follow"] != 5 {
		t.Errorf("the pig's breed, tempt and follow-parent priorities are %v, want 3, 4 and 5", got)
	}
	hand := Player{X: 0.5, Y: 64, Z: 6.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:carrot_on_a_stick"), Count: 1}}
	for range 200 {
		pig.Tick(w, []Player{hand})
	}
	if d := math.Hypot(pig.X-hand.X, pig.Z-hand.Z); d > 3 {
		t.Errorf("the pig stands %v from the player holding a carrot on a stick, want it come along", d)
	}
}

// temptingOnly strips a mob's goals but the tempting, so a test drives the
// lure alone without the stroll wandering the body between times.
func temptingOnly(m *Mob) *Mob {
	m.ai = goalSelector{}
	m.ai.wire(&temptGoal{m: m})
	return m
}

// TestATemptedCowStopsShortOfThePlayer checks the lure's stop distance: a
// cow walked in from eight blocks comes to rest around the vanilla 2.5 stop
// line instead of crowding the player's own position. The approach's own
// momentum carries it a little inside the line, so the body is measured a
// half block short of it.
func TestATemptedCowStopsShortOfThePlayer(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	cow := temptingOnly(NewMob(TypeCow, 1, 0.5, 64, 0.5, 2))
	hand := Player{X: 0.5, Y: 64, Z: 8.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}}
	for range 400 {
		cow.Tick(w, []Player{hand})
	}
	distance := math.Hypot(cow.X-hand.X, cow.Z-hand.Z)
	if distance < temptStopDistance-0.5 {
		t.Errorf("the tempted cow stood %v from the player, want it stop around the %v stop line", distance, temptStopDistance)
	}
	if distance > 4 {
		t.Errorf("the tempted cow stood %v from the player, want it come in to the stop distance", distance)
	}
}

// TestATemptedCowWaitsAfterTheLureIsGone checks the calm-down: once the
// lure is gone the goal lets go, and the lure's return does not set the cow
// off until the vanilla reducedTickDelay(100) wait has run.
func TestATemptedCowWaitsAfterTheLureIsGone(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	cow := temptingOnly(NewMob(TypeCow, 1, 0.5, 64, 0.5, 2))
	wheat := protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}
	near := Player{X: 0.5, Y: 64, Z: 8.5, HeldItem: wheat}
	for range 400 {
		cow.Tick(w, []Player{near})
	}
	if distance := math.Hypot(cow.X-near.X, cow.Z-near.Z); distance > 4 {
		t.Fatalf("the cow did not come in to the lure: it stood %v away", distance)
	}
	// The lure is gone long enough for the goal to stop, then the player
	// steps back out of reach and holds it again.
	empty := Player{X: 0.5, Y: 64, Z: 8.5}
	for range 10 {
		cow.Tick(w, []Player{empty})
	}
	far := Player{X: 0.5, Y: 64, Z: 15.5, HeldItem: wheat}
	still := math.Hypot(cow.X-far.X, cow.Z-far.Z)
	for range 30 {
		cow.Tick(w, []Player{far})
	}
	if moved := still - math.Hypot(cow.X-far.X, cow.Z-far.Z); moved > 0.5 {
		t.Errorf("the cow closed %v on the returning lure during its calm-down, want it wait", moved)
	}
	for range 400 {
		cow.Tick(w, []Player{far})
	}
	if distance := math.Hypot(cow.X-far.X, cow.Z-far.Z); distance > 4 {
		t.Errorf("the cow stood %v from the player after the calm-down, want it following", distance)
	}
}

// TestATemptedCowTurnsItsHeadToThePlayer checks the lure's LOOK flag: a cow
// walled in so its body cannot walk turns its head onto the player holding
// the wheat.
func TestATemptedCowTurnsItsHeadToThePlayer(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -5, -5, 10, 5, 63, stone)
	// A ring two blocks high around the cow's own cell: no path reaches
	// the player, so only the head can turn on them.
	for _, c := range [][2]int32{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}} {
		w.set(c[0], 64, c[1], stone)
		w.set(c[0], 65, c[1], stone)
	}

	cow := temptingOnly(NewMob(TypeCow, 1, 0.5, 64, 0.5, 2))
	hand := Player{X: 6.5, Y: 64, Z: 0.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}}
	cow.Yaw, cow.HeadYaw = 180, 180
	for range 20 {
		cow.Tick(w, []Player{hand})
	}
	want := yawToward(hand.X-cow.X, hand.Z-cow.Z)
	if diff := math.Abs(float64(cow.HeadYaw) - want); diff > 5 {
		t.Errorf("the tempted cow's head yaw = %v, want it turned onto the player at %v", cow.HeadYaw, want)
	}
}

// TestATemptedCowFollowsWheatInTheOffhand checks that a lure held in the
// off hand tempts as one in the main hand does.
func TestATemptedCowFollowsWheatInTheOffhand(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	cow := temptingOnly(NewMob(TypeCow, 1, 0.5, 64, 0.5, 2))
	hand := Player{X: 0.5, Y: 64, Z: 8.5, OffHandItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:wheat"), Count: 1}}
	for range 400 {
		cow.Tick(w, []Player{hand})
	}
	if distance := math.Hypot(cow.X-hand.X, cow.Z-hand.Z); distance > 4 {
		t.Errorf("the cow stood %v from the off-hand wheat, want it following", distance)
	}
}

// TestAChickenFollowsTheChickenFoodTag checks that the chicken's lure is
// read from the chicken_food tag, which numbers torchflower seeds and
// pitcher pod among its six items.
func TestAChickenFollowsTheChickenFoodTag(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)

	for _, name := range []string{"minecraft:torchflower_seeds", "minecraft:pitcher_pod"} {
		hen := temptingOnly(NewMob(TypeChicken, 1, 0.5, 64, 0.5, 2))
		hand := Player{X: 0.5, Y: 64, Z: 8.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, name), Count: 1}}
		for range 400 {
			hen.Tick(w, []Player{hand})
		}
		if distance := math.Hypot(hen.X-hand.X, hen.Z-hand.Z); distance > 5 {
			t.Errorf("the hen stood %v from the player holding %s, want it following", distance, name)
		}
	}
}

// TestAMobOutOfHealthDiesAndItsTickSaysSo checks the dying end of a mob:
// the hit that takes the last of the health marks it dead, its next tick
// reports the death once as an event, and nothing ticks after.
func TestAMobOutOfHealthDiesAndItsTickSaysSo(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -10, -10, 10, 10, 63, stone)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 3)
	if cow.Dead {
		t.Fatal("a fresh cow is dead, want it alive with its ten")
	}
	if !cow.TakeDamage(6, Player{X: 2.5, Y: 64, Z: 0.5}) {
		t.Fatal("the first hit did not land")
	}
	if cow.Dead || cow.Health != 4 {
		t.Fatalf("after six off ten: dead=%v health=%v, want alive at four", cow.Dead, cow.Health)
	}
	// The window ticks out — frightened the while, on a floor, alone.
	for range hurtCooldown + 1 {
		cow.Tick(w, nil)
	}
	if !cow.TakeDamage(4, Player{X: 2.5, Y: 64, Z: 0.5}) {
		t.Fatal("the mortal hit did not land")
	}
	if !cow.Dead || cow.Health != 0 {
		t.Fatalf("after the last four: dead=%v health=%v, want dead at zero", cow.Dead, cow.Health)
	}
	if cow.TakeDamage(1, Player{X: 2.5, Y: 64, Z: 0.5}) {
		t.Error("a hit on a dead mob landed, want it refused")
	}

	events := cow.Tick(w, nil)
	if len(events) != 1 || events[0].Kind != MobDied || events[0].Mob != cow {
		t.Fatalf("a dead mob's tick reported %v, want its one death", events)
	}
	if morbid := cow.Tick(w, nil); len(morbid) != 0 {
		t.Errorf("a dead mob ticked twice: %v, want death told once", morbid)
	}
}

// TestAChickenLaysAnEggWhenItsTimerComes checks the one per-tick
// happening a passive mob has beside the walking: the hen's countdown,
// its event, and the fresh countdown it rolls behind it.
func TestAChickenLaysAnEggWhenItsTimerComes(t *testing.T) {
	w := newTestWorld()
	chicken := NewMob(TypeChicken, 7, 0.5, 64, 0.5, 4)
	if chicken.EggTimer < eggIntervalMin || chicken.EggTimer > eggIntervalMax {
		t.Errorf("a fresh hen's egg timer = %v, want it within the vanilla span [%v, %v]",
			chicken.EggTimer, eggIntervalMin, eggIntervalMax)
	}

	chicken.EggTimer = 1
	events := chicken.Tick(w, nil)
	found := false
	for _, e := range events {
		if e.Kind == MobLaidEgg {
			found = true
		}
	}
	if !found {
		t.Error("the tick of the timer reported no egg, want it laid")
	}
	if chicken.EggTimer < eggIntervalMin || chicken.EggTimer > eggIntervalMax {
		t.Errorf("the next egg timer = %v, want it rolled fresh within the span", chicken.EggTimer)
	}
}

// TestAWaterloggedMobBobs checks the water half of the physics: dropped in
// over its head, a cow comes to no harm — the water resets the fall — and
// the buoyancy and the float goal hold it near the surface rather than
// settling it on the bottom.
func TestAWaterloggedMobBobs(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, 0, 0, 2, 2, 63, stone)
	for y := int32(64); y <= 68; y++ {
		floor(w, 0, 0, 2, 2, y, water)
	}

	cow := NewMob(TypeCow, 1, 1.5, 75, 1.5, 5)
	for range 300 {
		cow.Tick(w, nil)
	}
	if cow.OnGround {
		t.Fatalf("the cow settled on the pond's floor at y=%v, want it held afloat", cow.Y)
	}
	if cow.Y < 67.5 {
		t.Errorf("the cow floats at y=%v, want it near the surface at %v", cow.Y, 69)
	}
	if cow.Health != cow.MaxHealth {
		t.Errorf("the swim took health: %v, want the water a soft landing", cow.Health)
	}
}

// TestAMobsDropsRollWithinTheVanillaRanges rolls every type's drops many
// times over and checks each stack against the ranges the vanilla loot
// tables roll within: the items the tables name and none else, the counts
// the tables' set_count gave, and nothing dropped for a death nobody is
// behind.
func TestAMobsDropsRollWithinTheVanillaRanges(t *testing.T) {
	// The ranges are the vanilla loot tables' own: the uniform set_count
	// each table rolls its drop within, read off the data pack.
	ranges := map[MobType]map[string][2]int32{
		TypeCow:     {"minecraft:leather": {0, 2}, "minecraft:beef": {1, 3}},
		TypePig:     {"minecraft:porkchop": {1, 3}},
		TypeSheep:   {"minecraft:mutton": {1, 2}, "minecraft:white_wool": {1, 1}},
		TypeChicken: {"minecraft:feather": {0, 2}, "minecraft:chicken": {1, 1}},
		TypeZombie:  {"minecraft:rotten_flesh": {0, 2}},
		TypeSkeleton: {
			"minecraft:arrow": {0, 2},
			"minecraft:bone":  {0, 2},
		},
	}
	for _, mobType := range []MobType{TypeCow, TypePig, TypeSheep, TypeChicken, TypeZombie, TypeSkeleton} {
		want := ranges[mobType]
		if len(want) == 0 {
			t.Fatalf("the test has no vanilla ranges for %s", mobType)
		}
		m := NewMob(mobType, 1, 0.5, 64, 0.5, 6)
		if mobType == TypeSheep {
			// A sheep's wool rolls out of the colour table its colour
			// points at; this one is white, the colour most of the
			// vanilla flock is.
			m.Colour = "white"
		}
		seen := map[string]bool{}
		for range 300 {
			for _, stack := range m.RollDrops(rules.LootKiller{}, nil) {
				held, _ := registry.ItemName(stack.ItemID)
				rng, ok := want[held]
				if !ok {
					t.Fatalf("%s rolled %s, which its table does not name", mobType, held)
				}
				seen[held] = true
				if stack.Count < rng[0] || stack.Count > rng[1] {
					t.Errorf("%s rolled %d of %s, want the vanilla range [%d, %d]",
						mobType, stack.Count, held, rng[0], rng[1])
				}
			}
		}
		// Every drop that can come up has come up in three hundred rolls:
		// the chicken's zero-to-two feathers included, a zero rolling as
		// nothing rather than as an absence.
		for item := range want {
			if want[item][0] > 0 && !seen[item] {
				t.Errorf("%s never dropped its %s in three hundred rolls", mobType, item)
			}
		}
	}
}

// TestLootDoesNotFollowTheWandering kills two zombies of one seed, one of
// which spent its own dice on a long life first: the loot is the same.
func TestLootDoesNotFollowTheWandering(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		fresh := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		old := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		for range 1000 {
			old.Rand.Float64()
		}
		a, b := fresh.RollDrops(rules.LootKiller{}, nil), old.RollDrops(rules.LootKiller{}, nil)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("seed %d: the fresh zombie dropped %v, the old one %v", seed, a, b)
		}
	}
}

// TestAnAnimalMoansOnTheAnimalsSlowerRhythm checks the idle sound against
// each kind's own interval: each moan sends the counter back under the
// hundred and twenty Animal.getAmbientSoundInterval gives the animals, and
// the zombie back under Mob's eighty.
func TestAnAnimalMoansOnTheAnimalsSlowerRhythm(t *testing.T) {
	for _, kind := range []struct {
		name     string
		mobType  MobType
		interval int
		lo, hi   int
	}{
		// The band is the two thousand ticks at the kind's rhythm: the
		// counter's climb back plus the stretch the thousand-sided die
		// needs on top gives about twelve moans for the hundred and
		// twenty, sixteen for the eighty, with room either side.
		{"cow", TypeCow, 120, 6, 18},
		{"pig", TypePig, 120, 6, 18},
		{"sheep", TypeSheep, 120, 6, 18},
		{"chicken", TypeChicken, 120, 6, 18},
		{"zombie", TypeZombie, 80, 10, 22},
	} {
		c := NewMob(kind.mobType, 1, 0.5, 64, 0.5, 3)
		moans := 0
		for range 2000 {
			if c.moanStep() {
				moans++
				if c.ambientTimer != -kind.interval {
					t.Fatalf("a %s's moan left the timer at %d, want it back under the %d", kind.name, c.ambientTimer, kind.interval)
				}
			}
		}
		if moans < kind.lo || moans > kind.hi {
			t.Errorf("two thousand ticks produced %d moans for the %s, want %d to %d", moans, kind.name, kind.lo, kind.hi)
		}
	}
}

// TestTheAmbientVoiceSpeaksTheKindsOwnBabySound checks the idle sound's
// voice: a grown pig speaks its kind's event at the kind's volume, a piglet
// and a chick speak the baby events their getSoundSet swap in, and a calf
// keeps the grown event — the cow carries no baby set — at the quiet voice.
func TestTheAmbientVoiceSpeaksTheKindsOwnBabySound(t *testing.T) {
	pig := NewMob(TypePig, 1, 0.5, 64, 0.5, 3)
	event, volume, _ := pig.AmbientVoice()
	if event.Name() != "minecraft:entity.pig.ambient" || volume != 1.0 {
		t.Errorf("a pig's ambient voice is %s at %v, want its own at one", event.Name(), volume)
	}
	pig.SetBaby(true)
	if event, _, _ = pig.AmbientVoice(); event.Name() != "minecraft:entity.baby_pig.ambient" {
		t.Errorf("a piglet's ambient voice is %s, want the baby event", event.Name())
	}

	chick := NewMob(TypeChicken, 3, 0.5, 64, 2.5, 3)
	chick.SetBaby(true)
	if event, _, _ = chick.AmbientVoice(); event.Name() != "minecraft:entity.baby_chicken.ambient" {
		t.Errorf("a chick's ambient voice is %s, want the baby event", event.Name())
	}

	cow := NewMob(TypeCow, 2, 0.5, 64, 1.5, 3)
	cow.SetBaby(true)
	event, volume, _ = cow.AmbientVoice()
	if event.Name() != "minecraft:entity.cow.ambient" || volume != 0.4 {
		t.Errorf("a calf's ambient voice is %s at %v, want the grown event at the kind's 0.4", event.Name(), volume)
	}
}

// fallFrom drops a cow carrying the effect from the height onto a stone
// floor whose top face is at 64 and ticks until it rests, returning it.
func fallFrom(t *testing.T, y float64, in *effect.Instance) *Mob {
	t.Helper()
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, defaultState(t, "minecraft:stone"))
	cow := aimless(NewMob(TypeCow, 1, 0.5, y, 0.5, 1))
	if in != nil {
		if _, ok := cow.AddEffect(*in); !ok {
			t.Fatal("the cow refused the effect")
		}
	}
	for range 2000 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	return cow
}

// TestSlowFallingSparesAMobTheFall drops a cow from twenty blocks: with Slow
// Falling it floats down at the 0.01 gravity and lands unhurt, without it the
// same drop costs it its health.
// vanilla: LivingEntity.getEffectiveGravity, aiStep
func TestSlowFallingSparesAMobTheFall(t *testing.T) {
	slow := effect.New(registry.MustMobEffect("minecraft:slow_falling"), 100000, 0, false, true, true)
	cow := fallFrom(t, 84, &slow)
	if !cow.OnGround {
		t.Fatal("the slow-falling cow never landed")
	}
	if cow.Health != 10 {
		t.Errorf("health after a slow fall = %v, want it unhurt", cow.Health)
	}
	if bare := fallFrom(t, 84, nil); bare.Health >= 10 {
		t.Errorf("the control cow took no damage from twenty blocks")
	}
}

// TestLevitationLiftsAMob checks the rise: the velocity closes a fifth of the
// way to 0.05 a level each tick, and the fall it carried is written off.
// vanilla: LivingEntity.travelInAir
func TestLevitationLiftsAMob(t *testing.T) {
	w := newTestWorld()
	floor(w, -2, 0, 2, 2, 63, defaultState(t, "minecraft:stone"))
	cow := aimless(NewMob(TypeCow, 1, 0.5, 70, 0.5, 1))
	cow.FallDistance = 5
	lev := effect.New(registry.MustMobEffect("minecraft:levitation"), 100000, 1, false, true, true)
	cow.AddEffect(lev)
	cow.Tick(w, nil)
	if want := (0 + (0.05*2-0)*0.2) * verticalDrag; !closeEnough(cow.VY, want) {
		t.Errorf("vy after one tick = %v, want %v", cow.VY, want)
	}
	if cow.FallDistance != 0 {
		t.Errorf("fall distance under levitation = %v, want it written off", cow.FallDistance)
	}
	for range 40 {
		cow.Tick(w, nil)
	}
	if cow.Y <= 70 {
		t.Errorf("y after 41 ticks of levitation = %v, want it risen above 70", cow.Y)
	}
}

// TestJumpBoostRaisesAMobsSafeFall checks a six-block fall: Jump Boost II
// makes the safe distance five, so it costs a point less than the three free
// blocks would.
// vanilla: LivingEntity.calculateFallPower, MobEffects.JUMP_BOOST
func TestJumpBoostRaisesAMobsSafeFall(t *testing.T) {
	boost := effect.New(registry.MustMobEffect("minecraft:jump_boost"), 100000, 1, false, true, true)
	if cow := fallFrom(t, 70.5, &boost); cow.Health != 9 {
		t.Errorf("health after 6.5 blocks under jump boost II = %v, want 9 (safe distance 5)", cow.Health)
	}
	if cow := fallFrom(t, 70.5, nil); cow.Health != 7 {
		t.Errorf("health after 6.5 blocks bare = %v, want 7 (safe distance 3)", cow.Health)
	}
}

// TestSlowFallingCapsTheFallSpeedAndSparesTheRise pins the capped fall: the
// velocity follows the 0.01 gravity, while a body still
// rising falls under the plain 0.08.
// vanilla: LivingEntity.getEffectiveGravity
func TestSlowFallingCapsTheFallSpeedAndSparesTheRise(t *testing.T) {
	w := newTestWorld()
	slow := effect.New(registry.MustMobEffect("minecraft:slow_falling"), 100000, 0, false, true, true)

	falling := aimless(NewMob(TypeCow, 1, 0.5, 120, 0.5, 1))
	falling.AddEffect(slow)
	want := 0.0
	for range 40 {
		falling.Tick(w, nil)
		want = (want - 0.01) * float64(float32(verticalDrag))
	}
	if !closeEnough(falling.VY, want) {
		t.Errorf("vy after 40 slow-falling ticks = %v, want %v", falling.VY, want)
	}

	rising := aimless(NewMob(TypeCow, 2, 0.5, 120, 0.5, 1))
	rising.AddEffect(slow)
	rising.VY = 0.5
	rising.Tick(w, nil)
	if want := (0.5 - mobGravity) * float64(float32(verticalDrag)); !closeEnough(rising.VY, want) {
		t.Errorf("vy of a rising slow-falling cow = %v, want %v on the plain gravity", rising.VY, want)
	}
}

// TestLevitationWinsOverSlowFalling pins that a body carrying both rises on
// the Levitation lift alone.
// vanilla: LivingEntity.travelInAir
func TestLevitationWinsOverSlowFalling(t *testing.T) {
	w := newTestWorld()
	cow := aimless(NewMob(TypeCow, 1, 0.5, 120, 0.5, 1))
	cow.AddEffect(effect.New(registry.MustMobEffect("minecraft:slow_falling"), 100000, 0, false, true, true))
	cow.AddEffect(effect.New(registry.MustMobEffect("minecraft:levitation"), 100000, 0, false, true, true))
	cow.Tick(w, nil)
	if want := (0.05 * 0.2) * verticalDrag; !closeEnough(cow.VY, want) {
		t.Errorf("vy under both effects = %v, want %v", cow.VY, want)
	}
}

// TestAHappyGhastlingFlies checks HappyGhast.babyGhastSetup's flying mind:
// a hatched ghastling summoned high over the floor holds its height there
// and drifts off the cell it stood up in, vanilla's travel moving it by
// flying with no gravity to fall by. The height is set well clear of the
// floor so a body that fell could not be mistaken for one that flew.
// vanilla: HappyGhast.babyGhastSetup, HappyGhast.travel
func TestAHappyGhastlingFlies(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -2, 0, 2, 2, 63, stone)

	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)

	startX, startZ := ghastling.X, ghastling.Z
	for tick := range 100 {
		ghastling.Tick(w, nil)
		if ghastling.Y < 70 {
			t.Fatalf("the ghastling fell to %v after %d ticks, want it holding its height", ghastling.Y, tick)
		}
	}
	if math.Abs(ghastling.X-startX) < 0.1 && math.Abs(ghastling.Z-startZ) < 0.1 {
		t.Errorf("the ghastling stayed on its cell at (%v, %v), want it to drift off", ghastling.X, ghastling.Z)
	}
}

// TestAHappyGhastAdultHovers checks a grown happy ghast's flying mind: the
// adult runs the same flying travel its ghastling does, so a body summoned
// high over the floor holds its height without the ground's gravity pulling
// it down.
// vanilla: HappyGhast.travel
func TestAHappyGhastAdultHovers(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -2, 0, 2, 2, 63, stone)

	ghast := NewMob(TypeHappyGhast, 2, 0.5, 100, 0.5, 2)

	for tick := range 100 {
		ghast.Tick(w, nil)
		if ghast.Y < 70 {
			t.Fatalf("the happy ghast fell to %v after %d ticks, want it hovering", ghast.Y, tick)
		}
	}
}

// TestOnlyTheGhastlingTakesNoGravity checks who carries NoGravity: the
// FlyingMoveControl sets it on the body it drives, so a ghastling flying
// carries it, while the grown body's vanilla move control sets none and a
// fresh body is never born with it.
// vanilla: FlyingMoveControl.tick, Ghast.GhastMoveControl
func TestOnlyTheGhastlingTakesNoGravity(t *testing.T) {
	w := newTestWorld()

	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	for range 5 {
		ghastling.Tick(w, nil)
	}
	if !ghastling.NoGravity {
		t.Error("the ghastling flew without NoGravity, want its move control to set it")
	}

	ghast := NewMob(TypeHappyGhast, 2, 0.5, 100, 0.5, 2)
	for range 5 {
		ghast.Tick(w, nil)
	}
	if ghast.NoGravity {
		t.Error("the grown happy ghast carried NoGravity, want its move control to set none")
	}
}

// TestTheWanderOnlyPicksAirWithSomethingToDriftAgainst checks the wander's
// target test: a point is only picked where the body can be — its cell air —
// and only where a block stands within fifteen along one of the six
// directions, so a body is never sent into the rock.
// vanilla: Ghast.RandomFloatAroundGoal.isGoodTarget
func TestTheWanderOnlyPicksAirWithSomethingToDriftAgainst(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -20, -20, 20, 20, 63, stone)

	if flyTargetGood(w, 0.5, 63.5, 0.5) {
		t.Error("the wander called a point inside the floor good, want it turned away")
	}
	if !flyTargetGood(w, 0.5, 64.5, 0.5) {
		t.Error("the wander refused a point just over the floor, want it a target")
	}
	if flyTargetGood(w, 0.5, 200.5, 0.5) {
		t.Error("the wander called a point with nothing within fifteen blocks good, want it refused")
	}
}

// TestAHappyGhastlingFliesAtTheFlyingPace checks the flying drive's
// figures: held on a straight flight in the open air, a ghastling settles at
// vanilla's terminal, the per-tick drive (FLYING_SPEED times 5/3) over the
// air's 0.91 drag, about 0.046 blocks a tick rather than the fourfold the
// generic airborne speed once gave. The ghastling's move control drops the
// point the moment it has driven at it — one tick's flight per ask — so the
// goal's order is renewed before every tick, the way a running goal does.
// vanilla: FlyingMoveControl.tick, HappyGhast.travel
func TestAHappyGhastlingFliesAtTheFlyingPace(t *testing.T) {
	w := newTestWorld()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	ghastling.ai = goalSelector{}

	for range 60 {
		ghastling.seekToFly(100.5, 100, 0.5, 1.0)
		ghastling.Tick(w, nil)
	}
	before := ghastling.X
	for range 20 {
		ghastling.seekToFly(100.5, 100, 0.5, 1.0)
		ghastling.Tick(w, nil)
	}
	speed := (ghastling.X - before) / 20
	if speed < 0.035 || speed > 0.055 {
		t.Errorf("the ghastling flew at %v blocks a tick, want the vanilla terminal near 0.046", speed)
	}
}

// TestAHappyGhastlingHoldsItsPaceOnOneOrder checks that the standing order
// of one ask keeps driving the ghastling tick after tick, the flying
// navigation re-setting the move control's target as vanilla's does.
// vanilla: FlyingMoveControl.tick, PathNavigation.tick
func TestAHappyGhastlingHoldsItsPaceOnOneOrder(t *testing.T) {
	w := newTestWorld()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	ghastling.ai = goalSelector{}
	ghastling.seekToFly(200.5, 100, 0.5, 1.0)
	for range 100 {
		ghastling.Tick(w, nil)
	}
	if ghastling.X < 3 {
		t.Errorf("the ghastling flew %v blocks in 100 ticks on one order, want it to hold its pace", ghastling.X-0.5)
	}
}

// TestAHappyGhastlingFollowsAPlayerAtPace checks the whole brain over many
// ticks: a ghastling following a player twelve blocks off closes in at
// about the flying pace, not a block in two hundred ticks.
// vanilla: HappyGhastAi, FlyingMoveControl.tick
func TestAHappyGhastlingFollowsAPlayerAtPace(t *testing.T) {
	w := newTestWorld()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	player := Player{X: 0.5, Y: 100, Z: 12.5}
	for range 200 {
		ghastling.Tick(w, []Player{player})
	}
	if distance := math.Hypot(ghastling.X-player.X, ghastling.Z-player.Z); distance > 6 {
		t.Errorf("the ghastling is %v blocks from a player it followed for 200 ticks, want it close", distance)
	}
}

// TestAHappyGhastlingFliesThroughWater checks the issue's own case: a body
// standing in water still flies. HappyGhast.travel always takes the flying
// branch, so the water's own drag slows the drive but never hands the body
// to the walk physics, which would leave a body with no drive stranded where
// it stood.
// vanilla: HappyGhast.travel, LivingEntity.travelFlying
func TestAHappyGhastlingFliesThroughWater(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			for y := int32(60); y <= 74; y++ {
				w.set(x, y, z, water)
			}
		}
	}

	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 66, 0.5, 1)
	ghastling.SetBaby(true)

	startX, startZ := ghastling.X, ghastling.Z
	for range 100 {
		ghastling.Tick(w, nil)
	}
	if math.Hypot(ghastling.X-startX, ghastling.Z-startZ) < 0.1 {
		t.Errorf("the ghastling in water stayed at (%v, %v), want it flying through", ghastling.X, ghastling.Z)
	}
}

// TestAHappyGhastFollowsAHeldHarness checks the adult's lure: a grown happy
// ghast is tempted by anything on the happy_ghast_tempt_items tag — a
// harness as much as the snowball it eats — and follows it without a path,
// the lure vanilla registers for it alone.
// vanilla: HappyGhast.registerGoals, TemptGoal.ForNonPathfinders
func TestAHappyGhastFollowsAHeldHarness(t *testing.T) {
	w := newTestWorld()

	ghast := NewMob(TypeHappyGhast, 2, 0.5, 100, 0.5, 2)
	hand := Player{X: 0.5, Y: 100, Z: 14.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:white_harness"), Count: 1}}

	start := math.Hypot(ghast.X-hand.X, ghast.Z-hand.Z)
	for range 400 {
		ghast.Tick(w, []Player{hand})
	}
	if distance := math.Hypot(ghast.X-hand.X, ghast.Z-hand.Z); distance > start-1 {
		t.Errorf("the happy ghast closed from %v to %v on a held harness, want it come in", start, distance)
	}
}

// TestOnlyTheGhastlingAnswersTheFoodAlone checks the ghastling's lure: a baby
// happy ghast goes after the happy_ghast_food tag alone — the snowball it
// eats — and not the harnesses the grown body is tempted by, the tag
// HappyGhast.registerGoals picks for a baby. The follow of a player the brain
// also runs is left out, so only the lure moves the body.
// vanilla: HappyGhast.registerGoals, HappyGhastAi
func TestOnlyTheGhastlingAnswersTheFoodAlone(t *testing.T) {
	w := newTestWorld()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	aimless(ghastling)
	ghastling.ai.wire(&temptGoal{m: ghastling, reach: temptFlyReach, stopAt: babyTemptStop, speedFactor: babyTemptPace, nonPathfinding: true})

	harness := Player{X: 0.5, Y: 100, Z: 14.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:white_harness"), Count: 1}}
	start := math.Hypot(ghastling.X-harness.X, ghastling.Z-harness.Z)
	for range 200 {
		ghastling.Tick(w, []Player{harness})
	}
	if distance := math.Hypot(ghastling.X-harness.X, ghastling.Z-harness.Z); distance < start-0.1 {
		t.Errorf("the ghastling closed from %v to %v on a held harness, want the food alone to lure it", start, distance)
	}

	snowball := Player{X: 0.5, Y: 100, Z: 14.5, HeldItem: protocol.ItemStack{ItemID: itemID(t, "minecraft:snowball"), Count: 1}}
	start = math.Hypot(ghastling.X-snowball.X, ghastling.Z-snowball.Z)
	for range 400 {
		ghastling.Tick(w, []Player{snowball})
	}
	if distance := math.Hypot(ghastling.X-snowball.X, ghastling.Z-snowball.Z); distance > start-1 {
		t.Errorf("the ghastling closed from %v to %v on a held snowball, want it come in", start, distance)
	}
}

// TestTheGrownGhastDropsAPointItCannotReach checks the grown body's reach
// test: a point whose box would stand in the rock is dropped rather than
// pressed at forever — the move control's operation set to WAIT — so the next
// selection picks a fresh point, while a point in the open air is held.
// vanilla: Ghast.GhastMoveControl.canReach
func TestTheGrownGhastDropsAPointItCannotReach(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -4, -4, 4, 4, 63, stone)

	ghast := aimless(NewMob(TypeHappyGhast, 2, 0.5, 70, 0.5, 2))

	ghast.seekToFly(0.5, 55, 0.5, 1.0)
	ghast.Tick(w, nil)
	if ghast.nav.flyActive {
		t.Error("the grown happy ghast held a point under the floor, want the reach test to drop it")
	}

	ghast.seekToFly(0.5, 73, 0.5, 1.0)
	ghast.Tick(w, nil)
	if !ghast.nav.flyActive {
		t.Error("the grown happy ghast dropped a point in the open air, want it kept")
	}
}

// TestTheGrownGhastReachRefusesAFloorBelowIt checks the reach test's careful
// ring: the destination's box grown by one on every side, so a point whose
// feet would sit in or within a block of the floor is refused — the body is
// never sent to sink into the ground — while a point just clear of it is
// allowed.
// vanilla: Ghast.GhastMoveControl.canReach, blockTraversalPossible
func TestTheGrownGhastReachRefusesAFloorBelowIt(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -4, -4, 4, 4, 63, stone)

	ghast := aimless(NewMob(TypeHappyGhast, 2, 0.5, 70, 0.5, 2))

	if ghast.flyCanReach(w, 0, -6, 0) {
		t.Error("the reach test allowed a point whose box stands in the floor, want it refused")
	}
	if !ghast.flyCanReach(w, 0, -5, 0) {
		t.Error("the reach test refused a point clear of the floor, want it allowed")
	}
}

// TestTheGrownGhastDrivesInImpulses checks the grown body's drive: the
// GhastMoveControl presses at its point in whole flying-speed impulses every
// two to six ticks, not continuously, so a body held on a straight flight
// settles around a fifth of a block a tick — far above the ghastling's own
// terminal near 0.046.
// vanilla: Ghast.GhastMoveControl.tick
func TestTheGrownGhastDrivesInImpulses(t *testing.T) {
	w := newTestWorld()
	ghast := aimless(NewMob(TypeHappyGhast, 2, 0.5, 70, 0.5, 2))

	for range 60 {
		ghast.seekToFly(200.5, 70, 0.5, 1.0)
		ghast.Tick(w, nil)
	}
	before := ghast.X
	for range 60 {
		ghast.seekToFly(200.5, 70, 0.5, 1.0)
		ghast.Tick(w, nil)
	}
	speed := (ghast.X - before) / 60
	if speed < 0.14 || speed > 0.30 {
		t.Errorf("the grown happy ghast flew at %v blocks a tick, want the impulse drive near a fifth", speed)
	}
}

// TestTheGrownGhastRisesOutOfDeepWater checks the float goal's ask reaching
// the grown body's travel: a ghast standing in deep water with only the float
// goal wired rises on the jump block, where before the ask was spent nowhere
// and the body hung at the water's surface.
// vanilla: FloatGoal.tick, LivingEntity.aiStep, Mob.jumpInLiquid
func TestTheGrownGhastRisesOutOfDeepWater(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	floor(w, -6, -6, 6, 6, 63, stone)
	for x := int32(-6); x <= 6; x++ {
		for z := int32(-6); z <= 6; z++ {
			for y := int32(64); y <= 74; y++ {
				w.set(x, y, z, water)
			}
		}
	}

	ghast := NewMob(TypeHappyGhast, 2, 0.5, 64, 0.5, 2)
	aimless(ghast)
	ghast.ai.wire(newFloatGoal(ghast))

	startY := ghast.Y
	for range 100 {
		ghast.Tick(w, nil)
	}
	if ghast.Y <= startY+1 {
		t.Errorf("the grown happy ghast stayed at %v in deep water, want the float goal to lift it", ghast.Y)
	}
}

// TestTheGrownGhastWandersWithoutSinkingIntoTheFloor checks the reach test
// holding the wander off the ground: over a long wander over a floor the body
// never drives at a point that would sink it in, so its lowest feet stay
// clear of the block the floor's top is at.
// vanilla: Ghast.RandomFloatAroundGoal, Ghast.GhastMoveControl.canReach
func TestTheGrownGhastWandersWithoutSinkingIntoTheFloor(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	floor(w, -40, -40, 40, 40, 63, stone)

	ghast := NewMob(TypeHappyGhast, 2, 0.5, 70, 0.5, 2)

	minY := ghast.Y
	for range 600 {
		ghast.Tick(w, nil)
		if ghast.Y < minY {
			minY = ghast.Y
		}
	}
	if minY < 64.5 {
		t.Errorf("the grown happy ghast sank to %v over the floor, want the reach test to hold it clear", minY)
	}
}

// TestAHappyGhastlingDropsAnOrderOnceInThePointsBlock checks that the
// standing order ends when the body is in the point's block, not only when
// it is on the point to 0.0005 blocks, which a hovering body never is.
// vanilla: MoveToTargetSink.tick
func TestAHappyGhastlingDropsAnOrderOnceInThePointsBlock(t *testing.T) {
	w := newTestWorld()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	ghastling.ai = goalSelector{}
	ghastling.seekToFly(5.5, 100, 0.5, 1.0)
	for range 140 {
		ghastling.Tick(w, nil)
	}
	if ghastling.nav.flyActive {
		t.Errorf("the ghastling still holds an order at x=%v, want it dropped in the point's block", ghastling.X)
	}
}

// TestAHappyGhastlingGivesUpAWalledOffOrder checks that an order the body
// cannot fly to is dropped when its ticks run out.
// vanilla: MoveToTargetSink.start
func TestAHappyGhastlingGivesUpAWalledOffOrder(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for y := int32(90); y <= 110; y++ {
		for z := int32(-8); z <= 8; z++ {
			w.set(3, y, z, stone)
		}
	}
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 100, 0.5, 1)
	ghastling.SetBaby(true)
	ghastling.ai = goalSelector{}
	ghastling.seekToFly(10.5, 100, 0.5, 1.0)
	for range 260 {
		ghastling.Tick(w, nil)
	}
	if ghastling.nav.flyActive {
		t.Error("the ghastling still presses at the wall after 260 ticks, want the order timed out")
	}
}

// TestJumpBoostRaisesAMobsJump pins the hop's vy: 0.42 bare, 0.2 more under
// Jump Boost II, on a cube as well as a walker.
// vanilla: LivingEntity.getJumpPower, getJumpBoostPower
func TestJumpBoostRaisesAMobsJump(t *testing.T) {
	boost := effect.New(registry.MustMobEffect("minecraft:jump_boost"), 100000, 1, false, true, true)
	for _, typ := range []MobType{TypeCow, TypeSlime} {
		bare := NewMob(typ, 1, 0.5, 70, 0.5, 1)
		bare.jumpFromGround(1)
		boosted := NewMob(typ, 2, 0.5, 70, 0.5, 1)
		boosted.AddEffect(boost)
		boosted.jumpFromGround(1)
		if math.Abs(boosted.VY-bare.VY-0.2) > 1e-6 || !closeEnough(bare.VY, float64(float32(0.42))) && typ == TypeCow {
			t.Errorf("type %s: vy bare %v, boosted %v, want a 0.2 gap", typ, bare.VY, boosted.VY)
		}
	}
}

// TestPlayerCreditSurvivesAMobsBlow hurts a cow as a player and then as a
// mob inside the memory: the attacker becomes the mob, the credit stays the
// player's.
func TestPlayerCreditSurvivesAMobsBlow(t *testing.T) {
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	if !cow.TakeDamage(1, Player{X: 0.5, Y: 68, Z: 1.5, EntityID: 42}) {
		t.Fatal("the player's blow did not land")
	}
	cow.InvulnTicks = 0
	zombie := NewMob(TypeZombie, 7, 0.5, 68.5, 1.5, 1)
	if !cow.HurtByMob(1, zombie) {
		t.Fatal("the zombie's blow did not land")
	}
	if !cow.PlayerKillCredit() || cow.PlayerCreditID() != 42 {
		t.Errorf("credit = %v for %d, want the player 42", cow.PlayerKillCredit(), cow.PlayerCreditID())
	}
	if cow.AttackerID() != 7 {
		t.Errorf("attacker = %d, want the zombie 7", cow.AttackerID())
	}
}

// TestLastHurtByPlayerEndsWithAMobsBlow checks that a mob landing the last
// blow after a player's takes the catalyst credit away while the kill credit
// stays.
func TestLastHurtByPlayerEndsWithAMobsBlow(t *testing.T) {
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	if cow.LastHurtByPlayer() {
		t.Error("an unhurt cow was last hurt by a player")
	}
	cow.TakeDamage(1, Player{X: 0.5, Y: 68, Z: 1.5, EntityID: 42})
	if !cow.LastHurtByPlayer() {
		t.Error("the player's blow did not make the player the last to hurt")
	}
	cow.InvulnTicks = 0
	cow.HurtByMob(1, NewMob(TypeZombie, 7, 0.5, 68.5, 1.5, 1))
	if cow.LastHurtByPlayer() || !cow.PlayerKillCredit() {
		t.Errorf("after a zombie's blow: last hurt by player %v, kill credit %v; want false, true", cow.LastHurtByPlayer(), cow.PlayerKillCredit())
	}
}

// TestAPlayersHitKnocksAMobLikeLivingEntityKnockback checks the hit's shove:
// half the old horizontal motion stays, an airborne body keeps its vertical
// motion, a grounded one is lifted by the capped four tenths, and a hit from
// straight overhead still pushes sideways by the small random direction.
// vanilla: LivingEntity.knockback
func TestAPlayersHitKnocksAMobLikeLivingEntityKnockback(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0.5, 70, 0.5, 1)
	zombie.OnGround = false
	zombie.VX, zombie.VY = 0.6, -0.5
	if landed, _ := zombie.TakeDamageExtra(1, Player{X: 3.5, Y: 70, Z: 0.5}, 0); !landed {
		t.Fatal("the hit did not land")
	}
	if want := 0.3 - knockbackStrength; !closeEnough(zombie.VX, want) {
		t.Errorf("VX = %v, want half the old motion under the push %v", zombie.VX, want)
	}
	if !closeEnough(zombie.VY, -0.5) {
		t.Errorf("an airborne zombie's VY = %v, want its own -0.5 kept", zombie.VY)
	}

	ground := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 2)
	ground.OnGround = true
	ground.VY = 0.1
	ground.TakeDamageExtra(1, Player{X: 3.5, Y: 64, Z: 0.5}, 0)
	if !closeEnough(ground.VY, 0.4) {
		t.Errorf("a grounded zombie's VY = %v, want the cap 0.4", ground.VY)
	}

	over := NewMob(TypeZombie, 3, 0.5, 64, 0.5, 3)
	over.OnGround = true
	over.TakeDamageExtra(1, Player{X: 0.5, Y: 66, Z: 0.5}, 0)
	if h := math.Hypot(over.VX, over.VZ); !closeEnough(h, knockbackStrength) {
		t.Errorf("a hit from overhead pushed %v sideways, want %v in a random direction", h, knockbackStrength)
	}
}

// TestKnockbackResistanceScalesAMobsShove checks the kind's resistance cuts
// the strength before anything else: an iron golem, at 1.0, is not moved by
// a hit or an arrow's punch at all.
// vanilla: LivingEntity.knockback
func TestKnockbackResistanceScalesAMobsShove(t *testing.T) {
	golem := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 1)
	golem.OnGround = true
	if landed, _ := golem.TakeDamageExtra(1, Player{X: 3.5, Y: 64, Z: 0.5}, 0.5); !landed {
		t.Fatal("the hit did not land")
	}
	if golem.VX != 0 || golem.VZ != 0 || golem.VY != 0 {
		t.Errorf("an iron golem was shoved to (%v, %v, %v)", golem.VX, golem.VY, golem.VZ)
	}
	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	cow.OnGround = true
	cow.TakeDamageExtra(1, Player{X: 3.5, Y: 64, Z: 0.5}, 0)
	if cow.VX == 0 {
		t.Error("a cow with no resistance was not shoved")
	}
}

// TestKnockbackResistanceScalesAnArrowsPunch checks Punch's push is cut by
// the same resistance: none on a golem, half on a body at 0.5.
// vanilla: AbstractArrow.doKnockback
func TestKnockbackResistanceScalesAnArrowsPunch(t *testing.T) {
	golem := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 1)
	golem.arrowPunch(1, 0, 2)
	if golem.VX != 0 || golem.VY != 0 {
		t.Errorf("a golem was punched to (%v, %v)", golem.VX, golem.VY)
	}
	plain := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	plain.arrowPunch(1, 0, 2)
	half := NewMob(TypeCow, 3, 0.5, 64, 0.5, 3)
	half.SetKnockbackResistance(0.5)
	half.arrowPunch(1, 0, 2)
	if plain.VX <= 0 || !closeEnough(half.VX, plain.VX/2) {
		t.Errorf("punch pushed %v at resistance 0.5, want half of %v", half.VX, plain.VX)
	}
}

// TestACactusHurtsAMobStandingInIt puts a cow's box in a cactus cell: the
// tick bites a point and tells of it as a cactus burn.
func TestACactusHurtsAMobStandingInIt(t *testing.T) {
	w := newTestWorld()
	w.set(0, 68, 0, defaultState(t, "minecraft:cactus"))
	cow := aimless(NewMob(TypeCow, 1, 0.5, 68.5, 0.5, 1))
	before := cow.Health
	events := cow.Tick(w, nil)
	if cow.Health != before-1 {
		t.Errorf("health = %v, want %v", cow.Health, before-1)
	}
	if len(events) != 1 || events[0].Kind != MobBurned || events[0].DamageType != rules.DamageTypeCactus {
		t.Errorf("events = %v, want one cactus bite", events)
	}
}

// TestASpiderPackSharesOneHardEffect checks the pack's effect roll: on hard
// at full special multiplier some pack rolls one, every member carries the
// same, normal never rolls, and the cave spider never does.
// vanilla: Spider.finalizeSpawn
func TestASpiderPackSharesOneHardEffect(t *testing.T) {
	hard := rules.NewLocalDifficulty(rules.DifficultyHard, 4)
	normal := rules.NewLocalDifficulty(rules.DifficultyNormal, 4)
	pack := func(seed int64, typ MobType, local rules.LocalDifficulty) []*Mob {
		g := NewSpawnGroup(GoDice{rand.New(rand.NewSource(seed))})
		var out []*Mob
		for i := range 3 {
			m := NewMob(typ, int32(i+1), 0.5, 64, 0.5, int64(i+1))
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), g, local)
			out = append(out, m)
		}
		return out
	}
	rolled := 0
	for seed := range int64(200) {
		spiders := pack(seed, TypeSpider, hard)
		n := len(spiders[0].AllEffects())
		if n > 1 {
			t.Fatalf("seed %d: %d effects on one spider", seed, n)
		}
		for _, m := range spiders {
			all := m.AllEffects()
			if len(all) != n || (n == 1 && (all[0].ID != spiders[0].AllEffects()[0].ID || !all[0].IsInfinite())) {
				t.Fatalf("seed %d: the pack does not share one permanent effect", seed)
			}
		}
		if n == 1 {
			rolled++
		}
		{
			typ := TypeCaveSpider
			for _, m := range pack(seed, typ, hard) {
				if len(m.AllEffects()) != 0 {
					t.Fatalf("seed %d: a cave spider got an effect", seed)
				}
			}
		}
		for _, m := range pack(seed, TypeSpider, normal) {
			if len(m.AllEffects()) != 0 {
				t.Fatalf("seed %d: a normal-difficulty spider got an effect", seed)
			}
		}
	}
	if rolled == 0 || rolled > 60 {
		t.Errorf("%d of 200 hard packs rolled an effect, want about 20", rolled)
	}
}

// TestSpiderPackClaimAndNilGroup pins the pack guards: an animal never joins
// a pack a spider claimed, and a spider with no pack still rolls alone.
func TestSpiderPackClaimAndNilGroup(t *testing.T) {
	hard := rules.NewLocalDifficulty(rules.DifficultyHard, 4)
	for seed := range int64(50) {
		g := NewSpawnGroup(GoDice{rand.New(rand.NewSource(seed))})
		NewMob(TypeSpider, 1, 0.5, 64, 0.5, 1).FinalizeSpawn(GoDice{rand.New(rand.NewSource(1))}, testBiome("minecraft:plains"), g, hard)
		if !g.spider || !g.claimed {
			t.Fatalf("seed %d: the spider did not claim its pack", seed)
		}
		cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
		cow.FinalizeSpawn(GoDice{cow.Rand}, testBiome("minecraft:plains"), g, hard)
		if len(cow.AllEffects()) != 0 || g.size != 0 {
			t.Fatalf("seed %d: a cow joined the spider's pack", seed)
		}
	}
	rolled := 0
	for seed := range int64(200) {
		m := NewMob(TypeSpider, 1, 0.5, 64, 0.5, seed+1)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard)
		if len(m.AllEffects()) > 0 {
			rolled++
		}
	}
	if rolled == 0 {
		t.Error("a spider with no pack never rolled an effect")
	}
}

// TestAMobKeepsItsFallThroughARise checks that an upward move in mid-air
// neither subtracts from nor zeroes the fall a body carries, so the landing
// still hurts.
// vanilla: Entity.checkFallDamage
func TestAMobKeepsItsFallThroughARise(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 80.5, 0.5, 1))
	cow.FallDistance = 8
	cow.VY = 0.4
	cow.Tick(w, nil)
	if cow.Y <= 80.5 {
		t.Fatalf("y = %v, want the cow lifted", cow.Y)
	}
	if cow.FallDistance != 8 {
		t.Fatalf("fall distance after the rise = %v, want the 8 kept", cow.FallDistance)
	}
	for range 400 {
		cow.Tick(w, nil)
		if cow.OnGround {
			break
		}
	}
	if !cow.OnGround {
		t.Fatal("the cow never landed")
	}
	if cow.Health >= 10 {
		t.Errorf("health after the landing = %v, want the fall to hurt", cow.Health)
	}
}

// TestAMobLandingOnAnUpwardDripstoneTipIsImpaled checks PointedDripstoneBlock.fallOn
// for a mob: a one-block drop onto an upward tip hurts for 1 as stalagmite
// damage, and onto a downward tip not at all.
func TestAMobLandingOnAnUpwardDripstoneTipIsImpaled(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for _, tc := range []struct {
		direction string
		health    float64
		source    registry.DamageType
	}{
		{"up", 9, rules.DamageTypeStalagmite},
		{"down", 10, 0},
	} {
		w := newTestWorld()
		floor(w, -2, 0, 2, 2, 62, stone)
		w.set(0, 63, 0, registry.MustState("minecraft:pointed_dripstone", map[string]string{
			"vertical_direction": tc.direction, "thickness": "tip", "waterlogged": "false"}))
		cow := aimless(NewMob(TypeCow, 1, 0.5, 65.0, 0.5, 1))
		for range 400 {
			cow.Tick(w, nil)
			if cow.OnGround {
				break
			}
		}
		if !cow.OnGround {
			t.Fatalf("%s tip: the cow never came to rest", tc.direction)
		}
		if !closeEnough(cow.Health, tc.health) {
			t.Errorf("%s tip: health = %v, want %v", tc.direction, cow.Health, tc.health)
		}
		if tc.source != 0 && cow.lastDamageSource() != tc.source {
			t.Errorf("%s tip: last hurt = %q, want %q", tc.direction, cow.lastDamageSource(), tc.source)
		}
	}
}
