package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The firework rocket's flight as ordinary unit tests, driving the step
// over a world that is nothing but the blocks each case sets. No server is
// involved; the black-box tests in internal/server/server_fireworks_test.go remain
// the proof of the whole life of a rocket, and these are the cheapest
// checks of the flight's own hit test.

// TestAFireworkRocketPassesOverABodyItsLineBeginsInside births a rocket
// inside a body's box and walks it a few ticks: the projectile path's
// AABB.clip finds no crossing for a line that begins within the box, so the
// rocket rises through instead of bursting at once.
func TestAFireworkRocketPassesOverABodyItsLineBeginsInside(t *testing.T) {
	w := newTestWorld()
	// The body's box holds the rocket's own place, the way a rocket used on
	// the block under its launcher is born inside them.
	target := ArrowTarget{EntityID: 9, MinX: -1, MinY: 62, MinZ: -1, MaxX: 1, MaxY: 66, MaxZ: 1}
	f := NewFirework(1, 0, 64, 0, 0, 0, 1, 0, 3)

	for tick := 1; tick <= 3; tick++ {
		for _, ev := range f.Step(w, nil, nil, []ArrowTarget{target}) {
			if ev.Kind == FireworkBurst {
				t.Fatalf("the rocket burst on tick %d off a body its line began inside", tick)
			}
		}
		if f.Dead {
			t.Fatalf("the rocket was gone on tick %d", tick)
		}
	}
}

// TestAFireworkRocketCannotHitItsLauncherAtFirst drops a rocket back through
// the body of its own launcher — every block use's birth, the spawn beside
// the clicker — and checks the exclusion: the flight cannot touch them while
// it is still within their reach, and once it has flown clear and turned
// back the burst lands.
func TestAFireworkRocketCannotHitItsLauncherAtFirst(t *testing.T) {
	w := newTestWorld()
	launcher := PlayerTarget(7, 0, 64, 0, false)
	// Born just above the launcher's head, dropping back through them: the
	// line leaves their box for the crossing the sweep would find.
	f := NewFirework(1, 0, 66, 0, 7, 0, 1, 0, 3)
	f.VY = -1

	for range 3 {
		for _, ev := range f.Step(w, &launcher, nil, []ArrowTarget{launcher}) {
			if ev.Kind == FireworkBurst {
				t.Error("the rocket burst on its own launcher while still within their reach")
			}
		}
	}
	if f.leftOwner {
		t.Fatal("a rocket still within its launcher's reach is told it has left them")
	}

	// Flown clear of the launcher's own box: the mark is set.
	f.X, f.Y, f.Z = 0, 70, 0
	f.VX, f.VY, f.VZ = 0, 0, 0
	f.Step(w, &launcher, nil, []ArrowTarget{launcher})
	if !f.leftOwner {
		t.Fatal("a rocket clear of its launcher is still held unable to hit them")
	}

	// Turned back down across the launcher's body: with the mark set, the
	// hit lands.
	f.X, f.Y, f.Z = 0, 67, 0
	f.VX, f.VY, f.VZ = 0, -1, 0
	burst := false
	for range 3 {
		for _, ev := range f.Step(w, &launcher, nil, []ArrowTarget{launcher}) {
			if ev.Kind == FireworkBurst {
				burst = true
			}
		}
	}
	if !burst {
		t.Error("a rocket flown clear of its launcher and turned back never burst on them")
	}
}

// vanilla: FireworkRocketEntity.tick, Entity.collideWithShapes
func TestFireworkSlidesAlongWallBeforeHandlingItsHit(t *testing.T) {
	w := newTestWorld()
	w.set(1, 64, 1, registry.BlockStone.DefaultState())
	f := NewFirework(1, 0.5, 64, 0.5, 0, 0, 1, 0, 3)
	f.ShotAtAngle = true
	f.VX, f.VY, f.VZ = 1, 0, 1.25

	events := f.Step(w, nil, nil, nil)
	if len(events) != 2 || events[0].Kind != FireworkHitBlock || events[1].Kind != FireworkBurst {
		t.Fatalf("events = %+v, want a block hit and burst", events)
	}
	if burst := events[1]; burst.X != 0.875 || burst.Y != 64 || burst.Z != 1.75 {
		t.Errorf("burst position = (%v, %v, %v), want (0.875, 64, 1.75)", burst.X, burst.Y, burst.Z)
	}
	if !f.hitWall || f.VX != 1 || f.VY != 0 || f.VZ != 1.25 {
		t.Errorf("wall hit = %v, velocity = (%v, %v, %v), want a wall hit with the original velocity", f.hitWall, f.VX, f.VY, f.VZ)
	}
}
