package entity

import (
	"testing"
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
