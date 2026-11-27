package entity

import (
	"math"
	"math/rand"
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

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 2)
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

// TestABabyIsTheSmallerBoxItsKindGrowsOutOf checks the measures a baby is
// read from: the box and the eyes of the kind's own BABY_DIMENSIONS while
// the age is below nought, the grown ones the moment it is not — and the
// baby zombie's haste, the half again its modifier multiplies the base up
// to. The kinds that are never babies read the grown box whatever their
// age is poked to.
func TestABabyIsTheSmallerBoxItsKindGrowsOutOf(t *testing.T) {
	hen := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 1)
	if hen.width() != 0.4 || hen.height() != 0.7 || !closeEnough(hen.eyeHeight(), 0.644) {
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
	if hen.IsBaby() || hen.width() != 0.4 || !closeEnough(hen.eyeHeight(), 0.644) {
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

// temptingOnly strips a mob's goals but the tempting, so a test drives the
// lure alone without the stroll wandering the body between times.
func temptingOnly(m *Mob) *Mob {
	m.ai = goalSelector{}
	m.ai.wire(&temptGoal{m: m})
	return m
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

// movingGate moves a mob while the hurt asks it, as a plugin listener may.
type movingGate struct {
	to *Mob
	x  float64
}

func (g movingGate) Listening() bool { return true }
func (g movingGate) Ask(*HurtProbe)  { g.to.X = g.x }

// TestAMobBlowShovesFromTheAttackersPositionAfterTheGate: a listener that
// moves the attacker while the hurt is asked changes the shove's direction.
// vanilla: LivingEntity.hurtServer, DamageSource.getSourcePosition
func TestAMobBlowShovesFromTheAttackersPositionAfterTheGate(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	zombie := NewMob(TypeZombie, 2, 2.5, 64, 0.5, 2)
	cow.Gate = movingGate{to: zombie, x: -1.5}
	if !cow.HurtByMob(2, zombie) {
		t.Fatal("the blow did not land")
	}
	if cow.VX <= 0 {
		t.Errorf("cow vx = %v, want a shove away from the attacker's new place (+x)", cow.VX)
	}
}

func TestAdultFarmAnimalsUseTheirTypesEyeHeight(t *testing.T) {
	for _, c := range []struct {
		t    MobType
		want float64
	}{{TypeCow, 1.3}, {TypeMooshroom, 1.3}, {TypeSheep, 1.235}, {TypeChicken, 0.644}} {
		if got := NewMob(c.t, 1, 0, 0, 0, 1).EyeHeight(); got != c.want {
			t.Errorf("type %v: EyeHeight = %v, want %v", c.t, got, c.want)
		}
	}
}
