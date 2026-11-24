package entity

import (
	"testing"
)

// gapWorld is two stone platforms with a gap of three blocks between them,
// the only place a goat on the first can land that it cannot walk to.
func gapWorld(t *testing.T) testWorld {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-3); x <= 2; x++ {
			w.set(x, 63, z, stone)
		}
		for x := int32(6); x <= 10; x++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

func settledGoat(w World, seed int64) *Mob {
	g := NewMob(TypeGoat, 1, 2.5, 64, 0.5, seed)
	g.tick(w, nil, []*Mob{g})
	g.SetGoatRamCooldown(1 << 20)
	g.SetGoatLongJumpCooldown(0)
	return g
}

// The leap comes forty ticks after the landing spot is picked, with the goat
// standing where it was, and lands the goat across the gap.
// vanilla: LongJumpToRandomPos.tick
func TestAGoatLeapsFortyTicksAfterPickingASpot(t *testing.T) {
	w := gapWorld(t)
	g := settledGoat(w, 1)
	picked, launched := -1, -1
	var events []MobEvent
	for tick := 1; tick <= 400 && launched < 0; tick++ {
		events = append(events, g.tick(w, nil, []*Mob{g})...)
		if g.Goat.jump.chosen && picked < 0 {
			picked = tick
		}
		if g.Goat.jump.mid {
			launched = tick
		}
	}
	if picked < 0 || launched < 0 {
		t.Fatalf("picked at %d, launched at %d", picked, launched)
	}
	if launched-picked != 40 {
		t.Errorf("the leap came %d ticks after the pick, want 40", launched-picked)
	}
	if !g.Goat.jump.noFriction {
		t.Error("the launch kept friction")
	}
	for tick := 0; tick < 200 && g.Goat.jump.mid; tick++ {
		events = append(events, g.tick(w, nil, []*Mob{g})...)
		if tick == 1 && !g.LongJumping() {
			t.Error("the goat is not in the long jumping pose in flight")
		}
	}
	if g.X < 5.5 || !g.OnGround {
		t.Errorf("the goat landed at x=%.2f onGround=%v, want the far platform", g.X, g.OnGround)
	}
	if g.LongJumping() || g.Goat.jump.noFriction {
		t.Error("the pose or the friction outlived the landing")
	}
	if c := g.LongJumpCooldown(); c < 590 || c > goatJumpGapMax {
		t.Errorf("the cooldown after the landing = %d, want 600 to 1200", c)
	}
	var jump, step bool
	for _, e := range events {
		if e.Kind == MobGoatSounded {
			jump = jump || e.SoundID.Name() == "minecraft:entity.goat.long_jump"
			if e.SoundID.Name() == "minecraft:entity.goat.step" && e.Volume != 2 {
				t.Errorf("the landing step has volume %v, want 2", e.Volume)
			}
			step = step || e.SoundID.Name() == "minecraft:entity.goat.step"
		}
	}
	if !jump || !step {
		t.Errorf("long_jump sound %v, landing step %v", jump, step)
	}
}

func hunted(x, y, z float64) []Player {
	return []Player{{X: x, Y: y, Z: z, EntityID: 50, Huntable: true, Pushable: true}}
}

func flatWorld(t *testing.T) testWorld {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

// TestFrogLongJumpChecksPreferredBeforePathfinding verifies that a frog
// wanting a preferred block picks a lily pad on flat stone and, with no
// preferred block present, pathfinds at most once for the fallback spot rather
// than once per acceptable stone spot.
// vanilla: LongJumpToPreferredBlock.getJumpCandidate, LongJumpToRandomPos.pickCandidate
func TestFrogLongJumpChecksPreferredBeforePathfinding(t *testing.T) {
	t.Run("picks lily pad", func(t *testing.T) {
		w := flatWorld(t)
		w.set(5, 64, 0, defaultState(t, "minecraft:stone"))
		w.set(5, 65, 0, defaultState(t, "minecraft:lily_pad"))
		m := NewMob(TypeFrog, 1, 2.5, 64, 0.5, 1)
		m.OnGround = true
		var scratch MobPathScratch
		m.tickPaths = &scratch
		g := goatJumpGoal{m: m}
		view := tickView{w: w}
		g.Start(&view)
		m.frog.jump.wantPreferred = true
		g.pick(&view)
		if !m.frog.jump.chosen {
			t.Fatal("frog did not choose the lily pad spot")
		}
		if m.frog.jump.lookX != 5.5 || m.frog.jump.lookY != 66.5 || m.frog.jump.lookZ != 0.5 {
			t.Errorf("frog chose (%.1f, %.1f, %.1f), want lily pad spot (5.5, 66.5, 0.5)",
				m.frog.jump.lookX, m.frog.jump.lookY, m.frog.jump.lookZ)
		}
		if scratch.calls > 1 {
			t.Errorf("findPath calls = %d, want at most 1", scratch.calls)
		}
	})

	t.Run("no lily pad pathfinds once", func(t *testing.T) {
		w := flatWorld(t)
		// Find a seed whose first drawn spot is an acceptable flat stone cell
		// so the fallback spot reaches walkable/findPath.
		var calls int
		for seed := int64(1); seed <= 50; seed++ {
			m := NewMob(TypeFrog, 1, 2.5, 64, 0.5, seed)
			m.OnGround = true
			var scratch MobPathScratch
			m.tickPaths = &scratch
			g := goatJumpGoal{m: m}
			view := tickView{w: w}
			g.Start(&view)
			m.frog.jump.wantPreferred = true
			g.pick(&view)
			if m.frog.jump.chosen {
				t.Fatalf("seed %d: frog chose a jump to (%.1f, %.1f, %.1f) on walkable flat stone (calls=%d)",
					seed, m.frog.jump.lookX, m.frog.jump.lookY, m.frog.jump.lookZ, scratch.calls)
			}
			if scratch.calls > 1 {
				t.Fatalf("seed %d: findPath calls = %d, want at most 1", seed, scratch.calls)
			}
			if scratch.calls == 1 {
				calls = scratch.calls
				break
			}
		}
		if calls != 1 {
			t.Fatalf("no seed reached findPath on the fallback spot (calls=%d)", calls)
		}
	})
}
