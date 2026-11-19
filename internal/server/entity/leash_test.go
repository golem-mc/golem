package entity

import (
	"testing"

	"github.com/google/uuid"
)

// TestAPulledLeadCapsTheBankedFall checks the cap on the fall a dragged body
// has banked: a lead stretched to haul the body up caps the accumulation
// once the spring reaches it.
// vanilla: Leashable.onElasticLeashPull, Entity.checkFallDistanceAccumulation
func TestAPulledLeadCapsTheBankedFall(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0.5, 68, 0.5, 1)
	cow.LeashHolderID = 2
	cow.VY = -1
	cow.FallDistance = 5
	h := LeashHolder{EntityID: 2, X: 0.5, Y: 78, Z: 0.5,
		Width: 0.6, Height: 1.8, Present: true, SameLevel: true}
	cow.leash = &h

	cow.leashStep(newTestWorld(), nil)

	if cow.FallDistance > 1 {
		t.Errorf("the banked fall after a pull = %v, want it capped at 1", cow.FallDistance)
	}
}

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

// TestALeadComingOffADeadMobClearsTheHome: a dead body holds no lead, and
// the lead coming off takes the home with it.
// vanilla: Leashable.tickLeash, Mob.onLeashRemoved
func TestALeadComingOffADeadMobClearsTheHome(t *testing.T) {
	cow := homeCowWithALead()
	cow.Dead = true
	if events := cow.leashStep(newTestWorld(), nil); len(events) == 0 {
		t.Fatal("the dead body kept its lead")
	}
	if _, _, _, _, set := cow.HomeRestriction(); set {
		t.Error("the home outlived the dead body's lead")
	}
}

// TestALeadDroppedForAGoneHolderClearsTheHome: the lead with no holder to
// reach drops, and the home goes with it.
// vanilla: Leashable.tickLeash, Mob.onLeashRemoved
func TestALeadDroppedForAGoneHolderClearsTheHome(t *testing.T) {
	cow := homeCowWithALead()
	if events := cow.leashStep(newTestWorld(), nil); len(events) == 0 {
		t.Fatal("the lead without its holder was not dropped")
	}
	if _, _, _, _, set := cow.HomeRestriction(); set {
		t.Error("the home outlived the dropped lead")
	}
}

// TestALeadDroppedByAKilledMobClearsTheHome: the lead a blow killed the body
// out of comes off the same tick, and the home goes with it.
// vanilla: Leashable.tickLeash, Mob.onLeashRemoved
func TestALeadDroppedByAKilledMobClearsTheHome(t *testing.T) {
	cow := homeCowWithALead()
	if events := cow.deathLeashDrop(nil); len(events) == 0 {
		t.Fatal("the killed body kept its lead")
	}
	if _, _, _, _, set := cow.HomeRestriction(); set {
		t.Error("the home outlived the killed body's lead")
	}
}

// TestTheGraceOfACrossedLeadKeepsTheHome: the lead a dimension crossing
// carried is dropped once its grace passes without a holder claiming it —
// by the leash data coming away without the removal, so the home crossed
// with the body stands.
// vanilla: Leashable.restoreLeashFromSave
func TestTheGraceOfACrossedLeadKeepsTheHome(t *testing.T) {
	cow := homeCowWithALead()
	cow.CarryLeashAcross(2)
	for range LeashRestoreTicks + 1 {
		cow.leashStep(newTestWorld(), nil)
	}
	if cow.LeashHolderID != 0 {
		t.Fatalf("the crossed lead still holds entity %d after its grace", cow.LeashHolderID)
	}
	if _, _, _, _, set := cow.HomeRestriction(); !set {
		t.Error("the crossed lead's grace cleared the home, which outlives it")
	}
}

// TestOnlyAZombieNautilusWithAMobPassengerRefusesALead checks that a mob
// rider blocks the lead on a zombie nautilus but not on an ordinary one.
// vanilla: ZombieNautilus.canBeLeashed, Nautilus.canBeLeashed
func TestOnlyAZombieNautilusWithAMobPassengerRefusesALead(t *testing.T) {
	h := &LeashHolder{X: 0.5, Y: 68, Z: 0.5, Height: 1.8, Present: true, SameLevel: true}
	for _, tc := range []struct {
		typ  MobType
		want bool
	}{{TypeNautilus, true}, {TypeZombieNautilus, false}} {
		m := NewMob(tc.typ, 1, 0.5, 68, 0.5, 1)
		m.Passengers = append(m.Passengers, NewMob(TypeCow, 2, 0.5, 68, 0.5, 1))
		if got := LeashableBy(m, h); got != tc.want {
			t.Errorf("type %v with a passenger: LeashableBy = %v, want %v", tc.typ, got, tc.want)
		}
	}
}

// TestACamelHuskRefusesALeadOnlyWithAMobFirstPassenger checks that a mob
// rider makes a camel husk mob-controlled and unleashable, and a player
// rider seated ahead of it does not.
// vanilla: CamelHusk.isMobControlled, CamelHusk.canBeLeashed
func TestACamelHuskRefusesALeadOnlyWithAMobFirstPassenger(t *testing.T) {
	h := &LeashHolder{X: 0.5, Y: 68, Z: 0.5, Height: 1.8, Present: true, SameLevel: true}
	m := NewMob(TypeCamelHusk, 1, 0.5, 68, 0.5, 1)
	if !LeashableBy(m, h) {
		t.Fatal("an empty camel husk refuses a lead")
	}
	m.Passengers = append(m.Passengers, NewMob(TypeHusk, 2, 0.5, 68, 0.5, 1))
	if !m.MobControlled() || LeashableBy(m, h) {
		t.Error("a camel husk ridden by a husk is leashable or not mob-controlled")
	}
	m.Horse.Rider, m.Horse.RiderID = uuid.New(), 3
	if m.MobControlled() || !LeashableBy(m, h) {
		t.Error("a camel husk with a player first is mob-controlled or unleashable")
	}
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

func TestALeashedMobStopsTwoBlocksFromItsHolder(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	cow, h := aLeashedCowAt(4.5)
	cow.seekTo(w, 0.5, 64, -3.5, 1, navReach)
	if cow.navDone() {
		t.Fatal("set-up: the cow has no path")
	}
	h.Z = 2.0
	cow.leashStep(w, nil)
	if !cow.navDone() {
		t.Error("the cow keeps walking within two blocks of its holder")
	}
}

func TestALeashedAllayIsNotWalkedToItsHolder(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	cow, _ := aLeashedCowAt(4.5)
	cow.Type = TypeAllay
	cow.leashStep(w, nil)
	if !cow.navDone() {
		t.Error("the allay was ordered to walk")
	}
	if _, _, _, r, ok := cow.HomeRestriction(); r != 5 || !ok {
		t.Errorf("home radius %d set %v, want 5 true", r, ok)
	}
}
