package entity

import (
	"testing"
)

// TestADroppedLeadCarriesTheMobsPlaceOfTheDrop checks that the lead is laid
// where the mob stood when it came off, not where the rest of the tick moved
// the body to.
// vanilla: Leashable.tickLeash
func TestADroppedLeadCarriesTheMobsPlaceOfTheDrop(t *testing.T) {
	w, _ := pool(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 90, 0.5, 1))
	cow.LeashHolderID = 2
	cow.VX = 0.2

	var drop *MobEvent
	events := cow.Tick(w, nil)
	for i := range events {
		if events[i].Kind == MobLeashDropped {
			drop = &events[i]
		}
	}
	if drop == nil {
		t.Fatal("a lead without its holder was not dropped")
	}
	if cow.X == 0.5 {
		t.Fatal("the cow did not fall, so the test shows nothing")
	}
	if drop.MobX != 0.5 || drop.MobY != 90 || drop.MobZ != 0.5 {
		t.Errorf("the drop's place = (%v, %v, %v), want the cow's place at the drop (0.5, 90, 0.5)",
			drop.MobX, drop.MobY, drop.MobZ)
	}
}

// homeCowWithALead is a cow with its home around (0, 64, 0) and its lead
// held by entity 2.
func homeCowWithALead() *Mob {
	cow := NewMob(TypeCow, 1, 0.5, 68, 0.5, 1)
	cow.RestrictTo(0, 64, 0, 4)
	cow.LeashHolderID = 2
	return cow
}

func aLeashedCowAt(z float64) (*Mob, *LeashHolder) {
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.LeashHolderID = 2
	h := &LeashHolder{EntityID: 2, X: 0.5, Y: 64, Z: z, Width: 0.6, Height: 1.8,
		Present: true, SameLevel: true}
	cow.leash = h
	return cow, h
}

// TestALeashedMobWalksAfterItsHolder checks the walk toward the holder and
// the home a lead sets.
// vanilla: PathfinderMob.whenLeashedTo, PathfinderMob.closeRangeLeashBehaviour
func TestALeashedMobWalksAfterItsHolder(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	cow, _ := aLeashedCowAt(4.5)
	cow.leashStep(w, nil)
	if cow.navDone() {
		t.Error("the leashed cow has no path to its holder")
	}
	x, y, z, r, ok := cow.HomeRestriction()
	if x != 0 || y != 64 || z != 4 || r != 5 || !ok {
		t.Errorf("home = (%d, %d, %d, %d, %v), want (0, 64, 4, 5, true)", x, y, z, r, ok)
	}
}
