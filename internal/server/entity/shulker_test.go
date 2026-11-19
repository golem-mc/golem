package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// shulkerFloor is a stone slab at y 63 with air above.
func shulkerFloor(t *testing.T) testWorld {
	t.Helper()
	w := newTestWorld()
	w.diffUp, w.diff = true, 2
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-12); x <= 12; x++ {
		for z := int32(-12); z <= 12; z++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

// shulkerPlayer stands five blocks from the shulker.
var shulkerPlayer = []Player{{X: 5.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}

// openedShulker ticks a shulker until its attack goal has opened the shell
// fully. A glance at the player may shut it again, so the shell is read at
// the moment it opens.
func openedShulker(t *testing.T, w testWorld) *Mob {
	t.Helper()
	m := NewMob(TypeShulker, 1, 0.2, 64, 0.9, 3)
	for range 60 {
		m.Tick(w, shulkerPlayer)
		if m.ShulkerPeek() == shulkerOpenPeek {
			return m
		}
	}
	t.Fatalf("the shell never opened for a player five blocks away (peek %d)", m.ShulkerPeek())
	return nil
}

// TestAShulkerSitsOnTheCentreOfItsCellAndOpensForAPlayer: the spawn point is
// snapped to the block centre, and a player within reach opens the shell
// fully.
// vanilla: Shulker.setPos, ShulkerAttackGoal.start
func TestAShulkerSitsOnTheCentreOfItsCellAndOpensForAPlayer(t *testing.T) {
	m := openedShulker(t, shulkerFloor(t))
	if m.X != 0.5 || m.Y != 64 || m.Z != 0.5 {
		t.Errorf("the shulker stands at %v,%v,%v, want 0.5,64,0.5", m.X, m.Y, m.Z)
	}
	if m.ShulkerFace() != faceDown {
		t.Errorf("the attach face is %d, want down", m.ShulkerFace())
	}
}

// TestAShulkerLetsGoOfATargetBeyondTwentyBlocks shuts the shell once the
// player has walked out of the range the attack goal holds.
// vanilla: ShulkerAttackGoal.tick
func TestAShulkerLetsGoOfATargetBeyondTwentyBlocks(t *testing.T) {
	w := shulkerFloor(t)
	m := openedShulker(t, w)
	far := []Player{{X: 25.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}
	m.Tick(w, far)
	m.Tick(w, far)
	if m.TargetID != 0 {
		t.Errorf("the shulker still hunts a player 25 blocks away (target %d)", m.TargetID)
	}
}

// TestAClosedShulkerTurnsArrowsAndWearsArmor: only a shell that has been
// shut is armoured (20) and arrow-proof; an open one takes arrows.
// vanilla: Shulker.setRawPeekAmount, Shulker.hurtServer
func TestAClosedShulkerTurnsArrowsAndWearsArmor(t *testing.T) {
	w := shulkerFloor(t)
	m := openedShulker(t, w)
	if m.armoured(10) != 10 {
		t.Errorf("an open shell takes 10 as %v, want 10", m.armoured(10))
	}
	if landed, _ := m.HurtByArrow(2, 1, 0, 0, 9, true, false); !landed {
		t.Error("an open shulker turned an arrow")
	}
	m.Health = m.kind().maxHealth
	for range 200 {
		m.Tick(w, nil)
		if m.ShulkerClosed() {
			break
		}
	}
	if !m.ShulkerClosed() {
		t.Fatal("the shell never shut once the player left")
	}
	if got := m.armoured(10); got >= 10 {
		t.Errorf("a shut shell takes 10 as %v, want less", got)
	}
	// The window of the first arrow is over, so only the shell turns the
	// second.
	m.InvulnTicks = 0
	health := m.Health
	if landed, _ := m.HurtByArrow(2, 1, 0, 0, 9, true, false); landed || m.Health != health {
		t.Errorf("a shut shulker took an arrow: landed %v, health %v of %v", landed, m.Health, health)
	}
}

// TestAShulkerHurtsInAShutShellWithItsOwnSound: the shut shell cries
// hurt_closed and is silent between hurts; an open one cries the plain hurt.
// vanilla: Shulker.getHurtSound, playAmbientSound
func TestAShulkerHurtsInAShutShellWithItsOwnSound(t *testing.T) {
	w := shulkerFloor(t)
	m := openedShulker(t, w)
	if got := m.Sounds().Hurt.Name(); got != "minecraft:entity.shulker.hurt" {
		t.Errorf("an open shell hurts with %q", got)
	}
	m.setPeek(0, &tickView{w: w})
	if got := m.Sounds(); got.Hurt.Name() != "minecraft:entity.shulker.hurt_closed" || got.Ambient.Name() != "" {
		t.Errorf("a shut shell hurts with %q and speaks %q, want hurt_closed and silence", got.Hurt.Name(), got.Ambient.Name())
	}
}

// TestALandedHurtCanStartAShulkersTeleport: the roll runs from a hurt that
// lands, not only from the helper.
// vanilla: Shulker.hurtServer
func TestALandedHurtCanStartAShulkersTeleport(t *testing.T) {
	n := 0
	for seed := int64(1); seed <= 200; seed++ {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, seed)
		m.Health = 12
		if landed, _ := m.HurtByWorld(1, rules.DamageTypeMagic); !landed {
			t.Fatal("the hurt did not land")
		}
		if m.shulker.teleport {
			n++
		}
	}
	if n < 30 || n > 70 {
		t.Errorf("%d teleports of 200 landed hurts below half health, want about a quarter", n)
	}
}

// TestAShulkerLivesThroughPeaceful: its type is not notInPeaceful, so the
// difficulty neither removes it nor refuses its spawn.
// vanilla: EntityTypes.SHULKER, EntityType.canSpawn
func TestAShulkerLivesThroughPeaceful(t *testing.T) {
	w := shulkerFloor(t)
	w.diff = rules.DifficultyPeaceful
	if !AllowedAt(TypeShulker, rules.DifficultyPeaceful) {
		t.Error("peaceful refuses a shulker's spawn")
	}
	if AllowedAt(TypeZombie, rules.DifficultyPeaceful) {
		t.Error("peaceful allows a zombie's spawn")
	}
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	if m.despawnStep(w, nil) {
		t.Error("peaceful removed a shulker")
	}
	if !NewMob(TypeZombie, 2, 0.5, 64, 0.5, 3).despawnStep(w, nil) {
		t.Error("peaceful kept a zombie")
	}
}

// TestAStruckShulkerAlertsShulkersButIgnoresShulkerBlows: neighbours of its
// kind are sent after the attacker, and a blow from a shulker is not
// answered.
// vanilla: Shulker.registerGoals, HurtByTargetGoal.alertOthers
func TestAStruckShulkerAlertsShulkersButIgnoresShulkerBlows(t *testing.T) {
	w := shulkerFloor(t)
	shooter := NewMob(TypeSkeleton, 2, 9.5, 64, 0.5, 3)
	neighbour := NewMob(TypeShulker, 3, 3.5, 64, 3.5, 4)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 5)
	m.retaliate = shooter.ID
	for range 5 {
		m.tick(w, nil, []*Mob{shooter, neighbour})
	}
	if neighbour.TargetID != shooter.ID {
		t.Errorf("the neighbour targeted %d, want the skeleton %d", neighbour.TargetID, shooter.ID)
	}

	other := NewMob(TypeShulker, 4, 9.5, 64, 0.5, 6)
	struck := NewMob(TypeShulker, 5, 0.5, 64, 0.5, 7)
	struck.retaliate = other.ID
	for range 5 {
		struck.tick(w, nil, []*Mob{other})
	}
	if struck.TargetID != 0 {
		t.Errorf("a shulker answered a shulker's blow: target %d", struck.TargetID)
	}
}

// TestAnotherShulkerBlocksACellAShulkerWouldHold: the collision check counts
// other shulkers' hulls but never the mover's own.
// vanilla: Shulker.canStayAt, noCollision
func TestAnotherShulkerBlocksACellAShulkerWouldHold(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	// The open shell of a shulker on the floor reaches the cell above.
	if !m.canStayAt(shulkerWorld{World: w, self: m, others: []*Mob{m}}, m.cell(), faceDown) {
		t.Error("a shulker's own hull blocks its cell")
	}
	above := NewMob(TypeShulker, 2, 0.5, 65, 0.5, 4)
	if m.canStayAt(shulkerWorld{World: w, self: m, others: []*Mob{m, above}}, m.cell(), faceDown) {
		t.Error("a shulker in the cell its shell opens into did not block it")
	}
	m.Vehicle = above
	if !m.canStayAt(shulkerWorld{World: w, self: m, others: []*Mob{m, above}}, m.cell(), faceDown) {
		t.Error("a shulker's vehicle blocks its shell")
	}
}

// TestAShulkerHuntsOnlyInsideItsSlab: four blocks along the attach axis,
// measured from the shulker's own box.
// vanilla: Shulker.ShulkerNearestAttackGoal.getTargetSearchArea
func TestAShulkerHuntsOnlyInsideItsSlab(t *testing.T) {
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	in := shulkerInSlab(m)
	// The box tops out at y 65, so a player's feet may stand up to 69 and
	// their head reaches the slab from 60 - 1.8 = 58.2.
	if !in(nil, &Player{X: 0.5, Y: 68.9, Z: 0.5}) {
		t.Error("a player 3.9 blocks above the box is outside the slab")
	}
	if in(nil, &Player{X: 0.5, Y: 69.1, Z: 0.5}) {
		t.Error("a player 4.1 blocks above the box is inside the slab")
	}
	if !in(nil, &Player{X: 0.5, Y: 58.3, Z: 0.5}) || in(nil, &Player{X: 0.5, Y: 58.1, Z: 0.5}) {
		t.Error("the slab's lower edge is not four blocks below the box less the player's height")
	}
	if !in(nil, &Player{X: 14.5, Y: 64, Z: 0.5}) {
		t.Error("a player inside the follow range on the plane is outside the slab")
	}
}

// TestAShulkerWhoseFloorVanishesMovesToAnotherFace: the cell keeps a wall to
// cling to, so the face changes and the body stays; with nothing around it,
// it teleports within eight blocks.
// vanilla: Shulker.findNewAttachment
func TestAShulkerWhoseFloorVanishesMovesToAnotherFace(t *testing.T) {
	w := shulkerFloor(t)
	stone := defaultState(t, "minecraft:stone")
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.Tick(w, nil)
	w.set(0, 63, 0, 0)
	w.set(1, 64, 0, stone)
	m.Tick(w, nil)
	if m.ShulkerFace() != faceEast || m.X != 0.5 || m.Y != 64 {
		t.Errorf("face %d at %v,%v, want east at 0.5,64", m.ShulkerFace(), m.X, m.Y)
	}
	w.set(1, 64, 0, 0)
	// Each try picks five cells and most hang in the air.
	for range 60 {
		m.Tick(w, nil)
	}
	if m.X == 0.5 && m.Y == 64 && m.Z == 0.5 {
		t.Fatal("the shulker stayed in a cell with nothing to hold")
	}
	if dx, dy, dz := m.X-0.5, m.Y-64, m.Z-0.5; dx > 8 || dx < -8 || dy > 8 || dy < -8 || dz > 8 || dz < -8 {
		t.Errorf("teleported %v,%v,%v away, want within 8", dx, dy, dz)
	}
	cell := m.cell()
	if _, ok := m.attachableFace(shulkerWorld{World: w}, cell); !ok {
		t.Error("the shulker landed where no face holds it")
	}
}

// TestAHurtShulkerBelowHalfHealthSometimesTeleports: the quarter roll fires
// only under half health.
// vanilla: Shulker.hurtServer
func TestAHurtShulkerBelowHalfHealthSometimesTeleports(t *testing.T) {
	rolled := func(health float64) int {
		n := 0
		for seed := int64(1); seed <= 200; seed++ {
			m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, seed)
			m.Health = health
			m.noteShulkerHurt()
			if m.shulker.teleport {
				n++
			}
		}
		return n
	}
	if n := rolled(20); n != 0 {
		t.Errorf("%d teleports at 20 of 30 health, want none", n)
	}
	if n := rolled(10); n < 30 || n > 70 {
		t.Errorf("%d teleports of 200 at 10 health, want about a quarter", n)
	}
}

// TestAShulkerIsNotRemovedWhenFarAway: a shulker is a golem, so neither the
// distance nor the idle roll removes it, while a zombie in its place goes at
// once.
// vanilla: AbstractGolem.removeWhenFarAway, Mob.checkDespawn
func TestAShulkerIsNotRemovedWhenFarAway(t *testing.T) {
	w := shulkerFloor(t)
	far := []Player{survivor(7, 200.5)}
	shulker := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	for i := range 2000 {
		shulker.NoActionTicks = despawnNoActionTicks + 1
		if shulker.despawnStep(w, far) {
			t.Fatalf("the shulker 200 blocks from the player despawned on call %d", i+1)
		}
	}
	zombie := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 3)
	zombie.NoActionTicks = despawnNoActionTicks + 1
	if !zombie.despawnStep(w, far) {
		t.Error("a zombie built the same way stands, want it discarded past 128")
	}
}
