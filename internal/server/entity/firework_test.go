package entity

import "testing"

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

// TestAFireworkRocketGrantsItsTargetsTheAgeMargin flies a rocket past a
// body whose near face stands just off its line, the share a body three
// ticks old has earned: the first two ticks of flight grant none and pass
// it by, the third's own margin of a twentieth reaches it and the burst
// lands.
func TestAFireworkRocketGrantsItsTargetsTheAgeMargin(t *testing.T) {
	w := newTestWorld()
	// The box ahead and a twentieth aside of the line: without the margin
	// the line passes beside it, the third tick's margin grows it into
	// reach before the rocket's start is within it.
	target := ArrowTarget{EntityID: 9, MinX: 0.03, MinY: 63, MinZ: 5, MaxX: 0.6, MaxY: 65, MaxZ: 6}
	f := NewFirework(1, 0, 64, 0, 0, 0, 1, 0, 3)
	f.VZ = 1.5

	for tick := 1; tick <= 2; tick++ {
		for _, ev := range f.Step(w, nil, nil, []ArrowTarget{target}) {
			if ev.Kind == FireworkBurst {
				t.Fatalf("the rocket burst on tick %d, whose margin grants none", tick)
			}
		}
		if f.Dead {
			t.Fatalf("the rocket died before its third tick")
		}
	}
	burst := false
	for _, ev := range f.Step(w, nil, nil, []ArrowTarget{target}) {
		if ev.Kind == FireworkBurst {
			burst = true
		}
	}
	if !burst {
		t.Error("the rocket's third tick margin did not reach a body just off its line")
	}
}

// TestARidingFireworkRocketBurstsOnTheBodyInItsPath rides a starred rocket
// on a body flying forward past another: the attached rocket's flight is
// swept like the free one's, so it bursts on the body in its line.
func TestARidingFireworkRocketBurstsOnTheBodyInItsPath(t *testing.T) {
	w := newTestWorld()
	carrier := &FireworkCarrier{X: 0, Y: 64, Z: 0, VZ: 1.5}
	target := ArrowTarget{EntityID: 9, MinX: -1, MinY: 62, MinZ: 1, MaxX: 1, MaxY: 66, MaxZ: 10}
	f := NewFirework(1, 0, 64, 0, 0, 0, 1, 7, 3)

	for tick := 1; tick <= 5; tick++ {
		for _, ev := range f.Step(w, nil, carrier, []ArrowTarget{target}) {
			if ev.Kind == FireworkBurst {
				return
			}
		}
	}
	t.Error("a starred rocket riding a body through another never burst on it")
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

// TestFireworkStepReportsTheBlockItHits flies a crossbow rocket at a stone
// block one cell away: the step names the block and the face entered, for a
// starless rocket that lives on and a starred one that bursts after it.
// vanilla: FireworkRocketEntity.onHitBlock, Projectile.onHitBlock
func TestFireworkStepReportsTheBlockItHits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		explosions int32
	}{{"starless", 0}, {"starred", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			stone := defaultState(t, "minecraft:stone")
			w.set(1, 64, 0, stone)
			f := NewFirework(1, 0.5, 64.5, 0.5, 0, 0, tc.explosions, 0, 3)
			f.ShotAtAngle = true
			f.VX, f.VY, f.VZ = 1, 0, 0

			events := f.Step(w, nil, nil, nil)
			if len(events) == 0 || events[0].Kind != FireworkHitBlock {
				t.Fatalf("events = %+v, want a FireworkHitBlock first", events)
			}
			hit := events[0].BlockHit
			if hit.X != 1 || hit.Y != 64 || hit.Z != 0 || hit.State != stone {
				t.Errorf("hit = %+v, want the stone at (1,64,0)", hit)
			}
			if hit.Face != 4 { // west face of the block, entered travelling east
				t.Errorf("face = %d, want 4", hit.Face)
			}
			if hit.HitX != 1 {
				t.Errorf("HitX = %v, want 1", hit.HitX)
			}
			if tc.explosions == 0 {
				if f.Dead || len(events) != 1 {
					t.Errorf("starless rocket: dead=%v events=%+v, want alive with one event", f.Dead, events)
				}
				return
			}
			if !f.Dead || len(events) != 2 || events[1].Kind != FireworkBurst {
				t.Errorf("starred rocket: dead=%v events=%+v, want a burst after the hit", f.Dead, events)
			}
		})
	}
}

// TestAFireworkRocketPassesABodyWhoseBottomFaceItStartsOn births a rocket at
// a body's feet and flies it up through the body: the hit distance is zero,
// which the projectile path's AABB.clip does not count.
func TestAFireworkRocketPassesABodyWhoseBottomFaceItStartsOn(t *testing.T) {
	w := newTestWorld()
	target := ArrowTarget{EntityID: 9, MinX: -0.3, MinY: 64, MinZ: -0.3, MaxX: 0.3, MaxY: 65.8, MaxZ: 0.3}
	f := NewFirework(1, 0, 64, 0, 0, 0, 1, 0, 3)
	for tick := 1; tick <= 3; tick++ {
		for _, ev := range f.Step(w, nil, nil, []ArrowTarget{target}) {
			if ev.Kind == FireworkHitTarget || ev.Kind == FireworkBurst {
				t.Fatalf("the rocket burst on tick %d off a body whose feet it started at", tick)
			}
		}
	}
}
