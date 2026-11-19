package entity

import (
	"math"
	"testing"
)

// dividedPond is a water box, x 0..10, z 0..6 and two cells deep at y 62..63,
// that a stone wall at x=5 splits except for the gap at z=6.
func dividedPond(t testing.TB) testWorld {
	w := newTestWorld()
	stone, water := defaultState(t, "minecraft:stone"), defaultState(t, "minecraft:water")
	for x := int32(-1); x <= 11; x++ {
		for z := int32(-1); z <= 7; z++ {
			for y := int32(61); y <= 64; y++ {
				inside := x >= 0 && x <= 10 && z >= 0 && z <= 6 && y >= 62 && y <= 63
				switch {
				case !inside:
					w.set(x, y, z, stone)
				case x == 5 && z < 6:
					w.set(x, y, z, stone)
				default:
					w.set(x, y, z, water)
				}
			}
		}
	}
	return w
}

// TestASwimPathGoesThroughTheGapOfAWall: the search finds the way round a wall
// across the pond and ends within a block of the target.
// vanilla: PathFinder.findPath, SwimNodeEvaluator.getNeighbors
func TestASwimPathGoesThroughTheGapOfAWall(t *testing.T) {
	w := dividedPond(t)
	start, target := Waypoint{1, 62, 1}, Waypoint{9, 62, 1}
	path := findSwimPath(w, 1, 1, start, target, 1, 16, 256)
	if len(path) == 0 || path[0] != start {
		t.Fatalf("the path = %v, want it to open with the start", path)
	}
	end := path[len(path)-1]
	if manhattan(end, target) > 1 {
		t.Fatalf("the path ends at %v, want it within a block of %v", end, target)
	}
	gap := false
	for _, c := range path {
		if c.X == 5 && c.Z < 6 {
			t.Fatalf("the path crosses the wall at %v", c)
		}
		gap = gap || c.X == 5 && c.Z == 6
	}
	if !gap {
		t.Errorf("the path %v never uses the gap at z=6", path)
	}
}

// TestAFishSwimsRoundAWall: ordered to the far side of the wall, a cod follows
// its path through the gap and arrives. The stuck watch drops a long swim
// whose pace falls behind the move control's, so the order is renewed as the
// swim goal would.
// vanilla: WaterBoundPathNavigation, AbstractFish.FishMoveControl
func TestAFishSwimsRoundAWall(t *testing.T) {
	w := dividedPond(t)
	m := NewMob(TypeCod, 1, 1.5, 62, 1.5, 3)
	m.Tick(w, nil)
	for tick := range 3000 {
		if !m.fishOrdered() && !m.swimTo(w, 9.5, 62, 1.5, 1.0) {
			t.Fatalf("tick %d: the cod was given no path across the pond", tick)
		}
		m.Tick(w, nil)
		if math.Floor(m.X) == 5 && m.Z < 6 {
			t.Fatalf("tick %d: the cod is inside the wall at (%.2f, %.2f)", tick, m.X, m.Z)
		}
		if m.X > 8.5 {
			return
		}
	}
	t.Errorf("the cod ended at (%.2f, %.2f, %.2f), want it across the pond", m.X, m.Y, m.Z)
}

// openPond is a water box, x 0..10, z 0..4 and two cells deep at y 62..63,
// with a cod given the straight path (5,62,2), (6,62,2), (7,62,2) from x.
func openPond(t *testing.T, x float64) (testWorld, *Mob) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for bx := int32(0); bx <= 10; bx++ {
		for bz := int32(0); bz <= 4; bz++ {
			for y := int32(62); y <= 63; y++ {
				w.set(bx, y, bz, water)
			}
		}
	}
	m := NewMob(TypeCod, 1, x, 62, 2.5, 3)
	m.moveTo([]Waypoint{{5, 62, 2}, {6, 62, 2}, {7, 62, 2}}, Waypoint{7, 62, 2}, 1)
	m.nav.swim = true
	return w, m
}
