package entity

import (
	"container/heap"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
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

// BenchmarkSwimPath runs an open-water search across a 49x14x49 water box,
// from (0, 55, 0) to (9, 58, 7), range 16, 256 visited.
func BenchmarkSwimPath(b *testing.B) {
	w := newTestWorld()
	water := defaultState(b, "minecraft:water")
	for x := int32(-24); x <= 24; x++ {
		for z := int32(-24); z <= 24; z++ {
			for y := int32(49); y <= 62; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	start, target := Waypoint{0, 55, 0}, Waypoint{9, 58, 7}
	_ = findSwimPath(w, 1, 1, start, target, 1, 16, 256)
	b.ReportAllocs()
	for b.Loop() {
		_ = findSwimPath(w, 1, 1, start, target, 1, 16, 256)
	}
}

type refSwimOpen []swimEntry

func (h refSwimOpen) Len() int { return len(h) }
func (h refSwimOpen) Less(i, j int) bool {
	return h[i].f < h[j].f || h[i].f == h[j].f && h[i].order < h[j].order
}
func (h refSwimOpen) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *refSwimOpen) Push(x any)   { *h = append(*h, x.(swimEntry)) }
func (h *refSwimOpen) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// findSwimPathBreachingRef is the reference map-and-container/heap search
// that swimScratch must match node for node.
func findSwimPathBreachingRef(w World, ew, eh int32, start, target Waypoint, reach int32, maxLength float64, maxVisited int, breach bool) []Waypoint {
	nodes := map[Waypoint]*swimNode{}
	nodeAt := func(c Waypoint) *swimNode {
		n, ok := nodes[c]
		if !ok {
			n = &swimNode{cell: c}
			nodes[c] = n
		}
		return n
	}
	accepted := func(c Waypoint) *swimNode {
		n := nodeAt(c)
		if !n.accepted {
			n.accepted = true
			n.valid = swimCellOpen(w, ew, eh, c.X, c.Y, c.Z)
			if breach && rules.IsAir(w.BlockAt(c.X, c.Y, c.Z)) && waterPathfindable(w.BlockAt(c.X, c.Y-1, c.Z)) {
				n.valid = true
			}
		}
		if !n.valid {
			return nil
		}
		return n
	}
	var open refSwimOpen
	var counter int
	from := nodeAt(start)
	best, bestH := from, distanceTo(start, target)
	from.inOpen = true
	heap.Push(&open, swimEntry{node: from, f: bestH})
	for count := 1; open.Len() > 0 && count < maxVisited; count++ {
		cur := heap.Pop(&open).(swimEntry).node
		if cur.closed {
			continue
		}
		cur.inOpen = false
		cur.closed = true
		if manhattan(cur.cell, target) <= reach {
			break
		}
		if distanceTo(cur.cell, start) >= maxLength {
			continue
		}
		var around [6]*swimNode
		var found [10]*swimNode
		n := 0
		for i, d := range swimOrthogonal {
			node := accepted(Waypoint{cur.cell.X + d[0], cur.cell.Y + d[1], cur.cell.Z + d[2]})
			around[i] = node
			if node != nil && !node.closed {
				found[n] = node
				n++
			}
		}
		for i, d := range swimHorizontal {
			e := swimHorizontal[(i+1)%4]
			if !swimSideOpen(around, d) || !swimSideOpen(around, e) {
				continue
			}
			node := accepted(Waypoint{cur.cell.X + d[0] + e[0], cur.cell.Y, cur.cell.Z + d[1] + e[1]})
			if node != nil && !node.closed {
				found[n] = node
				n++
			}
		}
		for _, node := range found[:n] {
			step := distanceTo(cur.cell, node.cell)
			walked := cur.walked + step
			g := cur.g + step
			if breach && rules.IsAir(w.BlockAt(node.cell.X, node.cell.Y, node.cell.Z)) {
				g += 4 + 8
			}
			if walked >= maxLength || node.inOpen && !(g < node.g) {
				continue
			}
			node.from = cur
			node.g = g
			node.walked = walked
			h := distanceTo(node.cell, target)
			if h < bestH {
				best, bestH = node, h
			}
			counter++
			node.inOpen = true
			heap.Push(&open, swimEntry{node: node, f: g + h*swimHeuristicWeight, order: counter})
		}
	}
	var path []Waypoint
	for n := best; n != nil; n = n.from {
		path = append(path, n.cell)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func TestSwimPathScratchMatchesMapSearch(t *testing.T) {
	w := newTestWorld()
	stone, water := defaultState(t, "minecraft:stone"), defaultState(t, "minecraft:water")
	for x := int32(-18); x <= 18; x++ {
		for z := int32(-18); z <= 18; z++ {
			for y := int32(50); y <= 60; y++ {
				border := x == -18 || x == 18 || z == -18 || z == 18 || y == 50
				pillar := x%5 == 0 && z%5 == 0
				airPocket := y == 60 && x >= -10 && x <= 10 && z >= -10 && z <= 10 ||
					y >= 56 && y <= 57 && x >= 2 && x <= 6 && z >= -4 && z <= 4
				switch {
				case border || pillar:
					w.set(x, y, z, stone)
				case airPocket:
					// Leave as air above water to exercise BREACH nodes.
				default:
					w.set(x, y, z, water)
				}
			}
		}
	}

	rng := rand.New(rand.NewSource(633))
	ranges := []float64{0, 4, 8.5, 12, 16, 17, 24, 35}
	var scratch MobPathScratch
	for i := range 200 {
		start := Waypoint{int32(rng.Intn(33) - 16), int32(51 + rng.Intn(9)), int32(rng.Intn(33) - 16)}
		target := Waypoint{int32(rng.Intn(33) - 16), int32(51 + rng.Intn(10)), int32(rng.Intn(33) - 16)}
		maxLength := ranges[rng.Intn(len(ranges))]
		reach := int32(rng.Intn(2))
		ew, eh := int32(1+rng.Intn(2)), int32(1)
		maxVisited := max(1, int(math.Floor(maxLength*swimVisitedPerRange)))
		for _, breach := range []bool{false, true} {
			want := findSwimPathBreachingRef(w, ew, eh, start, target, reach, maxLength, maxVisited, breach)
			got := scratch.findSwimPath(w, ew, eh, start, target, reach, maxLength, maxVisited, breach)
			if !slices.Equal(got, want) {
				t.Fatalf("case %d (breach=%v start=%v target=%v range=%v reach=%d ew=%d): got %v, want %v",
					i, breach, start, target, maxLength, reach, ew, got, want)
			}
			if i%10 == 0 {
				pooled := (*MobPathScratch)(nil).findSwimPath(w, ew, eh, start, target, reach, maxLength, maxVisited, breach)
				if !slices.Equal(pooled, want) {
					t.Fatalf("case %d pooled (breach=%v): got %v, want %v", i, breach, pooled, want)
				}
			}
		}
	}
}
