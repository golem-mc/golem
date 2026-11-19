package entity

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The pathfinding as ordinary unit tests: the A* itself against worlds of
// the blocks each case lays. The walks found are followed in the physics
// tests of mob_test.go; these check the ways the search finds and the ways
// it honestly fails.

// pathMaxRange is the Chebyshev distance, in blocks, that the tests' direct
// findPath calls let a path wander from its first cell.
const pathMaxRange = 20

// vanilla: NodeEvaluator.getPathType, WalkNodeEvaluator.getPathTypeStatic
func TestStrafeWalkabilityClassifiesOnlyTheFeetCell(t *testing.T) {
	for _, tc := range []struct {
		name, support, feet, ceiling, neighbor string
		want                                   bool
	}{
		{"floor", "stone", "air", "air", "air", true},
		{"overhang", "stone", "air", "stone", "air", true},
		{"wall beside", "stone", "air", "air", "stone", true},
		{"no support", "air", "air", "air", "air", false},
		{"water support", "water", "air", "air", "air", false},
		{"lava support", "lava", "air", "air", "air", false},
		{"blocked feet", "stone", "stone", "air", "air", false},
		{"danger beside", "stone", "air", "air", "fire", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, defaultState(t, "minecraft:"+tc.support))
			w.set(0, 64, 0, defaultState(t, "minecraft:"+tc.feet))
			w.set(0, 65, 0, defaultState(t, "minecraft:"+tc.ceiling))
			w.set(1, 64, 0, defaultState(t, "minecraft:"+tc.neighbor))
			m := rangedSkeleton()
			if got := m.walkableCell(w, 0.5, 0.5); got != tc.want {
				t.Fatalf("walkable = %v, want %v", got, tc.want)
			}
		})
	}
}

// vanilla: PathNavigation.requiredPathLength, getMaxPathLength
func TestMaxPathLengthKeepsTheDefaultAndLargerRanges(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want float64
	}{
		{TypeVindicator, 16}, {TypeEvoker, 16}, {TypeCow, 16},
		{TypeZombie, 35}, {TypeFox, 32}, {TypeLlama, 40}, {TypeVillager, 48},
	} {
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 1)
		if got := m.maxPathLength(); got != tc.want {
			t.Errorf("%s path range = %v, want %v", tc.kind, got, tc.want)
		}
	}
	for _, tc := range []struct{ follow, want float64 }{
		{0, 16}, {8, 16}, {12, 16}, {16, 16}, {24, 24},
	} {
		m := NewMob(TypeVindicator, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeFollowRange, tc.follow)
		if got := m.maxPathLength(); got != tc.want {
			t.Errorf("follow range %v gives path range %v, want %v", tc.follow, got, tc.want)
		}
	}
}

// wall raises a two-block wall along a column of Z, two high so no step-up
// or one-block climb routes over it, leaving the doorway given open.
func wall(w testWorld, state uint16, x, z0, z1, door0, door1 int32, y int32) {
	for _, layer := range []int32{y, y + 1} {
		for z := z0; z <= z1; z++ {
			if door0 <= z && z <= door1 {
				continue
			}
			w.set(x, layer, z, state)
		}
	}
}

// TestAPathFindsItsWayAroundAnObstacle lays a wall between the start and
// the finish with one doorway in it, and checks the search routes through
// the doorway: found, every waypoint on walkable ground, and the door's own
// cells on the way.
func TestAPathFindsItsWayAroundAnObstacle(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	wall(w, stone, 5, -25, 25, -1, 0, 64)

	path, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{2, 64, 4}, Waypoint{8, 64, 4}, pathMaxRange, 0)
	if !ok {
		t.Fatal("no path around the wall with a door open in it, want one found")
	}
	if len(path) == 0 {
		t.Fatal("the path is empty, want it walked from start to finish")
	}
	if path[len(path)-1] != (Waypoint{8, 64, 4}) {
		t.Errorf("the path ends at %v, want the finish it was asked for", path[len(path)-1])
	}
	throughDoor := false
	for _, step := range path {
		if step.X == 5 && (step.Z == -1 || step.Z == 0) {
			throughDoor = true
		}
		if !walkableAt(w, strideOf(t, 0.9, 1.4), step.X, step.Y, step.Z) {
			t.Errorf("the path runs through %v, which is not a place to stand", step)
		}
	}
	if !throughDoor {
		t.Error("the path crossed the wall somewhere other than the door, want it through")
	}
}

// TestAPathIsNotFoundWhenTheTargetIsEnclosed seals a cell behind walls two
// blocks high and a roof above them — beyond a step, a hop and a three-block
// drop alike — and checks the search gives up rather than cheating.
func TestAPathIsNotFoundWhenTheTargetIsEnclosed(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)

	// Seal the cell (0, 64, 0) off: pillars of stone around it from the
	// ground to a roof, so nothing climbs, drops or squeezes in.
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if x == 0 && z == 0 {
				continue
			}
			for y := int32(64); y <= 66; y++ {
				w.set(x, y, z, stone)
			}
		}
	}
	w.set(0, 66, 0, stone) // the roof over the cell itself

	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{4, 64, 4}, Waypoint{0, 64, 0}, pathMaxRange, 0); ok {
		t.Error("a path was found into a sealed cell, want the search honest")
	}
}

// TestAPathNeverStepsOntoAFenceTop lays an oak fence, a cobblestone wall
// and a closed fence gate each across the way and holds every one of them:
// their collision tops stand 1.5 over the floor they rise from, past the
// 1.125 the ordinary jump reaches, and vanilla's
// WalkNodeEvaluator.findAcceptedNode takes no neighbour whose floor
// outruns the mob's jump height — so the way stops at the barrier's foot
// and never plans the step onto the top.
func TestAPathNeverStepsOntoAFenceTop(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	gate, ok := registry.FindState("minecraft:oak_fence_gate", map[string]string{"open": "false", "facing": "north"})
	if !ok {
		t.Fatal("the block registry has no closed oak fence gate")
	}
	for _, tc := range []struct {
		name  string
		block uint16
	}{
		{"oak fence", defaultState(t, "minecraft:oak_fence")},
		{"cobblestone wall", defaultState(t, "minecraft:cobblestone_wall")},
		{"closed oak fence gate", gate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -25, -25, 25, 25, 63, stone)
			// The barrier runs the whole search range, so there is no way
			// around it, only over it.
			for z := int32(-25); z <= 25; z++ {
				w.set(5, 64, z, tc.block)
			}
			cow := strideOf(t, 0.9, 1.4)
			path, ok := findPath(w, cow, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0)
			if ok {
				t.Error("a path was found over the barrier, want the search to hold its top against the jump")
			}
			if len(path) == 0 {
				t.Fatal("the way is empty, want it walked to the barrier's foot")
			}
			for _, step := range path {
				if ext := blockExtent(w.BlockAt(step.X, step.Y-1, step.Z)); ext > 1.0 {
					t.Errorf("the way steps onto %v, floored by a block whose top stands %v over its base", step, ext)
				}
				if !walkableAt(w, cow, step.X, step.Y, step.Z) {
					t.Errorf("the way runs through %v, which is no place to stand", step)
				}
			}
			if end := path[len(path)-1]; end != (Waypoint{4, 64, 0}) {
				t.Errorf("the way ends at %v, want the foot of the barrier", end)
			}
		})
	}

	// A fence laid at the floor's own level is a step up of only its top
	// over the floor the body leaves, within the jump height, so the search
	// takes it: vanilla's findAcceptedNode holds the fence's own cell
	// blocked but the cell above it walkable, and no rise beyond the jump
	// stands between them.
	fence := defaultState(t, "minecraft:oak_fence")
	for _, tc := range []struct {
		name  string
		block uint16
	}{
		{"farmland", defaultState(t, "minecraft:farmland")},
		{"dirt path", defaultState(t, "minecraft:dirt_path")},
		{"soul sand", defaultState(t, "minecraft:soul_sand")},
		{"bottom slab", slabState(t)},
		{"enchanting table", defaultState(t, "minecraft:enchanting_table")},
	} {
		t.Run(tc.name+" floor", func(t *testing.T) {
			w := newTestWorld()
			floor(w, -25, -25, 25, 25, 64, tc.block)
			for z := int32(-25); z <= 25; z++ {
				w.set(5, 64, z, fence)
			}
			cow := strideOf(t, 0.9, 1.4)
			path, ok := findPath(w, cow, Waypoint{2, 65, 0}, Waypoint{8, 65, 0}, pathMaxRange, 0)
			if !ok {
				t.Fatal("no path over a fence laid level with the floor, want the step taken")
			}
			overFence := false
			for _, step := range path {
				if step.X == 5 {
					overFence = true
				}
				if !walkableAt(w, cow, step.X, step.Y, step.Z) {
					t.Errorf("the way runs through %v, which is no place to stand", step)
				}
			}
			if !overFence {
				t.Error("the way never crossed the fence, want it stepped over")
			}
			if end := path[len(path)-1]; end != (Waypoint{8, 65, 0}) {
				t.Errorf("the way ends at %v, want the finish past the barrier", end)
			}
		})
	}
}

// TestAPathClimbsOneAndDropsThreeNoFarther checks the slopes one waypoint
// may take: a one-block climb is a way, a drop of three is a way, and a
// drop of four is not one for a walker.
func TestAPathClimbsOneAndDropsThreeNoFarther(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()

	// A mesa: ground at 64 for x<2, a plateau with its top at 65 for x>=2,
	// and beyond it a floor with its top at 62 edges the map's far side.
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-6); x <= 1; x++ {
			w.set(x, 63, z, stone)
		}
		for x := int32(2); x <= 5; x++ {
			w.set(x, 64, z, stone)
			w.set(x, 61, z, stone) // the plateau drops into a floor three below
		}
		for x := int32(6); x <= 9; x++ {
			w.set(x, 61, z, stone)
		}
	}

	// Up one, from the ground onto the plateau.
	if path, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{1, 64, 0}, Waypoint{2, 65, 0}, pathMaxRange, 0); !ok {
		t.Error("no path up a one-block step, want it climbed")
	} else if len(path) != 1 || path[0] != (Waypoint{2, 65, 0}) {
		t.Errorf("the climb came out as %v, want the single step onto the plateau", path)
	}

	// Down three, from the plateau's edge to the floor below it, and back
	// off the plateau the way it came up.
	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{5, 65, 0}, Waypoint{6, 62, 0}, pathMaxRange, 0); !ok {
		t.Error("no path down a three-block drop, want it taken")
	}
	if path, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{2, 65, 0}, Waypoint{1, 64, 0}, pathMaxRange, 0); !ok {
		t.Error("no path back down off the plateau, want it stepped down")
	} else if len(path) != 1 || path[0] != (Waypoint{1, 64, 0}) {
		t.Errorf("the step down came out as %v, want the single cell off the plateau", path)
	}
}

// TestAFarPathIsBeyondTheSearchesRange checks the honest failure of the
// search's own bounds: a target farther out than the search ever looks is
// not found, without so much as a look around.
func TestAFarPathIsBeyondTheSearchesRange(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -60, -60, 60, 60, 63, stone)

	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{0, 64, 0}, Waypoint{3, 64, 0}, pathMaxRange, 0); !ok {
		t.Error("no path three blocks along a floor, want the trivial walk found")
	}
	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{0, 64, 0}, Waypoint{40, 64, 0}, pathMaxRange, 0); ok {
		t.Error("a path forty blocks out was found, want it beyond the range")
	}
}

// TestAFailedSearchKeepsTheWayToItsBestNode plants a pillar four high on
// an open floor — its top a place to stand and no walker's way up — and
// checks the fallback a search that cannot reach such a place keeps: the
// way to the best node it opened, the cell of the pillar's foot, in the
// same walked order a found way comes back in. From the foot itself not
// even that stands: the cell the search rates best is the one the body is
// already on, and the way left is empty.
func TestAFailedSearchKeepsTheWayToItsBestNode(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	pillarFor(t, w, 0, 0)

	zombie := strideOf(t, 0.6, 1.95)
	path, ok := findPath(w, zombie, Waypoint{-6, 64, 0}, Waypoint{0, 68, 0}, pathMaxRange, 0)
	if ok {
		t.Error("a path was found to the top of a pillar four high, want the search honest about it")
	}
	if len(path) == 0 {
		t.Fatal("the failed search kept no way, want the best node it opened")
	}
	for _, step := range path {
		if !walkableAt(w, zombie, step.X, step.Y, step.Z) {
			t.Errorf("the partial way runs through %v, which is not a place to stand", step)
		}
	}
	end := path[len(path)-1]
	ring := max(max(end.X, -end.X), max(end.Z, -end.Z))
	if end.Y != 64 || ring != 1 {
		t.Errorf("the partial way ends at %v, want the pillar's own foot", end)
	}

	// From the foot, the pillar beside the start itself: the best cell is
	// the one under the walker.
	if path, ok := findPath(w, zombie, Waypoint{-1, 64, 0}, Waypoint{0, 68, 0}, pathMaxRange, 0); ok || len(path) != 0 {
		t.Errorf("the search from the pillar's foot came back as %v (reached %v), want no way at all", path, ok)
	}
}

// TestAPathToASolidCellKeepsItsBestNode asks for a path into a cell a block
// fills — a place no walker can stand — and checks the search runs anyway
// and keeps the way to the cell nearest it, rather than answering with
// nothing before it looks.
func TestAPathToASolidCellKeepsItsBestNode(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	w.set(5, 64, 0, stone) // the target cell itself is solid

	foot := strideOf(t, 0.9, 1.4)
	path, ok := findPath(w, foot, Waypoint{0, 64, 0}, Waypoint{5, 64, 0}, pathMaxRange, 0)
	if ok {
		t.Error("the search reached a cell a block fills, want it honest")
	}
	if len(path) == 0 {
		t.Fatal("no way kept to a target nobody can stand in, want the best node")
	}
	if end := path[len(path)-1]; end != (Waypoint{4, 64, 0}) {
		t.Errorf("the way ends at %v, want the cell before the filled one", end)
	}

	// A reach of one counts the cell beside the filled target as reaching
	// it, so the same ask answers with a path and a yes.
	if path, ok := findPath(w, foot, Waypoint{0, 64, 0}, Waypoint{5, 64, 0}, pathMaxRange, 1); !ok || len(path) == 0 || path[len(path)-1] != (Waypoint{4, 64, 0}) {
		t.Errorf("the search with a one-block reach came out as %v (reached %v), want the cell beside the filled one", path, ok)
	}
}

// TestAPathPastTheSearchesRangeKeepsItsBestNode asks for a path to a target
// beyond the range the search may wander, and checks it runs to the edge of
// the range and keeps the way there rather than answering with nothing.
func TestAPathPastTheSearchesRangeKeepsItsBestNode(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -60, -60, 60, 60, 63, stone)

	from, to := Waypoint{0, 64, 0}, Waypoint{40, 64, 0}
	path, ok := findPath(w, strideOf(t, 0.9, 1.4), from, to, pathMaxRange, 0)
	if ok {
		t.Error("the search reached a target past its range, want the honest no")
	}
	if len(path) == 0 {
		t.Fatal("no way kept toward a target past the range, want the best node")
	}
	if end := path[len(path)-1]; chebyshev(from, end) != pathMaxRange {
		t.Errorf("the way ends at %v, %d from the start, want the range's own edge %d", end, chebyshev(from, end), pathMaxRange)
	}
}

// TestAPathStopsAtItsReach asks for a path with a reach of one and checks the
// search considers the target reached at the edge of the reach — a node
// within one block, vanilla's PathFinder target test — rather than walking
// the last cell onto it. The same ask without a reach walks the whole way.
func TestAPathStopsAtItsReach(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	foot := strideOf(t, 0.9, 1.4)

	path, ok := findPath(w, foot, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 1)
	if !ok {
		t.Fatal("no path within a one-block reach, want it found")
	}
	if len(path) == 0 || path[len(path)-1] != (Waypoint{7, 64, 0}) {
		t.Errorf("the path came out as %v, want it stopping one block short at (7,64,0)", path)
	}

	exact, ok := findPath(w, foot, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0)
	if !ok || len(exact) == 0 || exact[len(exact)-1] != (Waypoint{8, 64, 0}) {
		t.Errorf("the exact search came out as %v (reached %v), want the target's own cell", exact, ok)
	}
}

// TestAReachedPathEndsAtItsBestNode sets a one-block step beside the way to a
// target higher still and asks with a reach that trips on a plain-floor node
// first: the reached way ends at the best node the search opened — the step's
// top, nearer the target — rather than at the node that tripped the reach. On
// a flat floor the two are always the same cell, the node that trips the
// reach being the nearest one opened, so only an obstacle such as this step
// can tell them apart.
func TestAReachedPathEndsAtItsBestNode(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	w.set(0, 64, -4, stone) // a one-block step beside the way

	foot := strideOf(t, 0.9, 1.4)
	path, ok := findPath(w, foot, Waypoint{3, 64, -4}, Waypoint{-1, 67, -5}, pathMaxRange, 3)
	if !ok {
		t.Fatal("no path within the reach, want one found")
	}
	if len(path) == 0 {
		t.Fatal("the reached path is empty, want the way to its best node")
	}
	if end := path[len(path)-1]; end != (Waypoint{0, 65, -4}) {
		t.Errorf("the reached way ends at %v, want the best node on the step (0,65,-4)", end)
	}
}

// TestAPathWalksOnFromAStartOverWater stands a mob on the surface of a
// one-deep pond — a cell it cannot itself stand in — and asks for the pond
// cell below with a reach of one. The start is within that reach, so vanilla
// would answer with the one-node path at the start and leave the mob on the
// water; the search steps off to a water cell the body can end in instead,
// the deliberate departure from the reach test that findPath's doc names.
func TestAPathWalksOnFromAStartOverWater(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 63, stone)
	// A pond one deep, its bed a layer under the plain's floor.
	for _, cell := range [][2]int32{{2, 0}, {3, 0}, {2, 1}, {3, 1}} {
		w.set(cell[0], 62, cell[1], stone)
		w.set(cell[0], 63, cell[1], water)
	}
	foot := strideOf(t, 0.9, 1.4)

	path, ok := findPath(w, foot, Waypoint{2, 64, 0}, Waypoint{2, 63, 0}, pathMaxRange, 1)
	if !ok {
		t.Fatal("no path from the water's surface into the pond, want one")
	}
	if len(path) == 0 {
		t.Fatal("the path is empty, want the mob walked off the water's surface")
	}
	if end := path[len(path)-1]; end.Y != 63 || end.X < 2 || end.X > 3 || end.Z < 0 || end.Z > 1 {
		t.Errorf("the path ends at %v, want a water cell of the pond", end)
	}
}

// TestAFloatingMobPathsOutOfADeepPond floats a body in the middle of a pond
// two deep and asks for the bank above it. Without the canFloat half of
// vanilla's WalkNodeEvaluator.getFloorLevel the surface reads its floor from
// the water's bed two blocks down, the one-block bank outruns the jump
// height and the body is trapped; a body that may float reads the water's
// own half-block top and walks the surface out, the way over water a wading
// body has no floor to take.
func TestAFloatingMobPathsOutOfADeepPond(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 61, stone)
	// The pond: water two deep over its bed, and the bank two blocks higher
	// to the east.
	for z := int32(-8); z <= 8; z++ {
		for x := int32(-8); x <= 3; x++ {
			w.set(x, 62, z, water)
			w.set(x, 63, z, water)
		}
		for x := int32(4); x <= 8; x++ {
			w.set(x, 62, z, stone)
			w.set(x, 63, z, stone)
		}
	}

	floating := strideOf(t, 0.9, 1.4)
	floating.floats = true
	path, reached := findPath(w, floating, Waypoint{0, 63, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0)
	if !reached {
		t.Fatal("a body that may float found no way out of the pond, want it on the bank")
	}
	if end := path[len(path)-1]; end != (Waypoint{8, 64, 0}) {
		t.Errorf("the way out ends at %v, want the bank (8,64,0)", end)
	}

	// The same body without the float reads the bed as its floor and is
	// held in the pond, the bank out of its jump.
	sinking := strideOf(t, 0.9, 1.4)
	if _, reached := findPath(w, sinking, Waypoint{0, 63, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0); reached {
		t.Error("a body that may not float climbed out of the pond, want the bank out of its reach")
	}
}

// strideOf is the pathing footprint the kinds the tests name stand their
// paths with, the floor(width + 1) across and floor(height + 1) up vanilla's
// node evaluator counts for a body. The body's own width and height ride
// along, the figures the risen step's box check is measured with.
func strideOf(t *testing.T, width, height float64) footprint {
	t.Helper()
	return footprintOf(width, height)
}

// TestAWideMobWillNotSqueezeThroughANarrowGap plants a wall across the way
// with a door one cell wide in it, and holds the width the body walks with:
// the zombie's one-cell stride steps through the door, while the spider,
// twice as wide, finds no way at all.
func TestAWideMobWillNotSqueezeThroughANarrowGap(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	wall(w, stone, 5, -25, 25, 0, 0, 64) // the door: z = 0 alone

	zombie := strideOf(t, 0.6, 1.95)
	path, ok := findPath(w, zombie, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0)
	if !ok {
		t.Fatal("no path through a one-wide door for the one-wide zombie, want it walked")
	}
	throughDoor := false
	for _, step := range path {
		if step.X == 5 && step.Z == 0 {
			throughDoor = true
		}
		if !walkableAt(w, zombie, step.X, step.Y, step.Z) {
			t.Errorf("the path runs through %v, which is not a place to stand", step)
		}
	}
	if !throughDoor {
		t.Error("the path crossed the wall somewhere other than the door, want it through")
	}

	spider := strideOf(t, 1.4, 0.9)
	if _, ok := findPath(w, spider, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0); ok {
		t.Error("a path through a one-wide door for the two-wide spider, want the search honest")
	}
}

// TestAWideMobWalksWhereItsFootprintFits widens the same wall's door to the
// two cells the spider's stride needs, and holds that the spider now walks
// — and that it walks the side of the door its stride fits, every waypoint
// standing with both its cells of breadth clear.
func TestAWideMobWalksWhereItsFootprintFits(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	wall(w, stone, 5, -25, 25, -1, 0, 64) // the door: z = -1 and z = 0

	spider := strideOf(t, 1.4, 0.9)
	path, ok := findPath(w, spider, Waypoint{2, 64, -1}, Waypoint{8, 64, -1}, pathMaxRange, 0)
	if !ok {
		t.Fatal("no path through a two-wide door for the two-wide spider, want it walked")
	}
	for _, step := range path {
		if !walkableAt(w, spider, step.X, step.Y, step.Z) {
			t.Errorf("the spider's path runs through %v, whose footprint meets a block", step)
		}
	}
	if path[len(path)-1] != (Waypoint{8, 64, -1}) {
		t.Errorf("the path ends at %v, want the finish it was asked for", path[len(path)-1])
	}

	// The same door stays a doorway for the one-wide zombie, whose single
	// cell of stride fits either side of it.
	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0); !ok {
		t.Error("no path through a two-wide door for the one-wide zombie, want it walked")
	}
}

// TestAFlatMobWalksUnderALowCeiling roofs a whole room one block above its
// floor and holds the height the body stands with: the spider, nine tenths
// of a block tall, walks the room's whole length under the roof it would
// brush, while the zombie's head and a half meets the roof everywhere and
// finds no way at all.
func TestAFlatMobWalksUnderALowCeiling(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 63, stone)
	floor(w, -6, -6, 10, 6, 65, stone) // the roof, one block over heads

	spider := strideOf(t, 1.4, 0.9)
	if _, ok := findPath(w, spider, Waypoint{-5, 64, 0}, Waypoint{9, 64, 0}, pathMaxRange, 0); !ok {
		t.Error("no path under a one-block ceiling for the flat spider, want it walked")
	}
	if _, ok := findPath(w, strideOf(t, 0.9, 1.4), Waypoint{-5, 64, 0}, Waypoint{9, 64, 0}, pathMaxRange, 0); ok {
		t.Error("a path under a one-block ceiling for the tall zombie, want the search honest")
	}
}

// TestAStepUpUnderALowCeilingIsNoWayUp stands a one-block step before a
// body with a roof one block over its own head and holds the risen box
// vanilla's WalkNodeEvaluator takes a climb with: the jump the step asks
// leaves the column the body stands in, so the room the climb needs is the
// room over the body, not the room over the step. The cow's and the sheep's
// head and a half meet that roof and the step is no way up at all, while
// the chicken's seven tenths fits under it and climbs.
func TestAStepUpUnderALowCeilingIsNoWayUp(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	cow := strideOf(t, 0.9, 1.4)
	sheep := strideOf(t, 0.9, 1.3)
	chicken := strideOf(t, 0.4, 0.7)

	// A three-by-three pen walled two high all around but the east side's
	// gate — the gate's lower block left solid as the step, its upper
	// block open as the way through — with the roof hanging over the
	// column the climb starts from, one block of headroom over its feet.
	step := func(roofY int32) testWorld {
		w := newTestWorld()
		floor(w, -25, -25, 25, 25, 63, stone)
		for x := int32(1); x <= 4; x++ {
			for z := int32(-2); z <= 2; z++ {
				edge := x == 1 || x == 4 || z == -2 || z == 2
				if edge {
					w.set(x, 64, z, stone)
					w.set(x, 65, z, stone)
				}
			}
		}
		w.set(4, 65, 0, 0) // the gate: the step below, the way through above
		w.set(3, roofY, 0, stone)
		return w
	}

	if _, ok := findPath(step(66), cow, Waypoint{2, 64, 0}, Waypoint{4, 65, 0}, pathMaxRange, 0); ok {
		t.Error("a path up the step under the roof over the cow's own column, want the climb refused")
	}
	if _, ok := findPath(step(66), sheep, Waypoint{2, 64, 0}, Waypoint{4, 65, 0}, pathMaxRange, 0); ok {
		t.Error("a path up the step under the roof over the sheep's own column, want the climb refused")
	}
	if _, ok := findPath(step(66), chicken, Waypoint{2, 64, 0}, Waypoint{4, 65, 0}, pathMaxRange, 0); !ok {
		t.Error("no path up the step for the chicken its roof leaves room for, want it climbed")
	}
	if _, ok := findPath(step(67), cow, Waypoint{2, 64, 0}, Waypoint{4, 65, 0}, pathMaxRange, 0); !ok {
		t.Error("no path up the step when the roof hangs a block higher, want it climbed")
	}

	// The roof over the step top itself the walkable check already
	// refuses, roof over the climber or none.
	over := newTestWorld()
	floor(over, -25, -25, 25, 25, 63, stone)
	over.set(5, 64, 0, stone)
	over.set(5, 66, 0, stone)
	if _, ok := findPath(over, cow, Waypoint{4, 64, 0}, Waypoint{5, 65, 0}, pathMaxRange, 0); ok {
		t.Error("a path onto a step with a roof over the step top, want the stand refused")
	}
}

// TestAPathThatCannotBeWalkedIsGivenUp hands a cow a path and then walls it
// in, and holds the giving up vanilla's PathNavigation.doStuckDetection
// makes of a body that stops making way: a hundred ticks apart the walk
// covered is held against a quarter of the pace's own hundred ticks of
// travel, and a walk that covers less is dropped whole.
func TestAPathThatCannotBeWalkedIsGivenUp(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.Tick(w, nil) // the settle that lands it
	cow.seekTo(w, 8.5, 64, 0.5, 1.0, 0)
	if len(cow.nav.path) == 0 {
		t.Fatal("no path in hand after the way was offered, want one")
	}

	// Wall the pen in: stone around the cow's column from its feet to
	// its head, so no step, climb or squeeze leads out, and the path
	// cannot be walked a single cell of its way.
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if x == 0 && z == 0 {
				continue
			}
			w.set(x, 64, z, stone)
			w.set(x, 65, z, stone)
		}
	}

	dropped := false
	for range 400 {
		cow.Tick(w, nil)
		if len(cow.nav.path) == 0 {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Error("the path survived 400 ticks against walls it cannot walk, want it given up")
	}

	// And the giving up holds: with every way out sealed, no new path
	// comes to take the dropped one's place.
	for range 200 {
		cow.Tick(w, nil)
	}
	if len(cow.nav.path) != 0 {
		t.Error("a path came back after the walls were sealed, want the giving up kept")
	}
}

// TestASeekKeepsAStandingPathOnlyForTheSameBlock checks PathNavigation.
// createPath's guard on the path a goal holds: while the path is not done and
// was planned to the same target block it is kept unchanged, and an ask for
// another block plans afresh.
func TestASeekKeepsAStandingPathOnlyForTheSameBlock(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.Tick(w, nil) // the settle that lands it
	if !cow.seekTo(w, 8.5, 64, 0.5, 1.0, 0) {
		t.Fatal("no path in hand after the way was offered, want one")
	}
	standing := cow.nav.path[len(cow.nav.path)-1]
	if standing != (Waypoint{8, 64, 0}) {
		t.Fatalf("the first way ends at %v, want the target block (8,64,0)", standing)
	}

	// A nudge inside the same block keeps the standing way.
	if !cow.seekTo(w, 8.9, 64, 0.6, 1.0, 0) {
		t.Fatal("the ask to the same block was refused")
	}
	if last := cow.nav.path[len(cow.nav.path)-1]; last != standing {
		t.Errorf("the ask inside the same block replanned to %v, want the standing way kept", last)
	}

	// The next block plans again rather than keeping the way to the block
	// it holds.
	if !cow.seekTo(w, 9.5, 64, 0.5, 1.0, 0) {
		t.Fatal("the ask to the next block was refused")
	}
	if last := cow.nav.path[len(cow.nav.path)-1]; last != (Waypoint{9, 64, 0}) {
		t.Errorf("the ask to the next block kept the way ending at %v, want a fresh plan to (9,64,0)", last)
	}
}

// TestAWideMobStepsOffTheEdgeToReachTheFoot lays a shelf one block over the
// floor with a wide walker on its lip and the goal at the foot below, and
// checks the way runs straight off the side rather than around it: the
// walker's footprint reaches past the lip, so a floor under the footprint's
// far cells alone (WalkNodeEvaluator.getPathTypeOfMob) still makes the cell
// beyond the edge a step. The plant case lays short grass over the goal and
// the lip, which a non-colliding block must not turn back into an obstacle.
func TestAWideMobStepsOffTheEdgeToReachTheFoot(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	grass := defaultState(t, "minecraft:short_grass")
	for _, plant := range []bool{false, true} {
		name := "bare"
		if plant {
			name = "short grass"
		}
		t.Run(name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -6, -6, 6, 6, 63, stone)
			// The shelf: two cells across and three deep, its top one
			// block above the floor.
			for x := int32(1); x <= 2; x++ {
				for z := int32(-1); z <= 1; z++ {
					w.set(x, 64, z, stone)
				}
			}
			if plant {
				w.set(0, 64, 0, grass)
				w.set(1, 65, 0, grass)
			}

			spider := strideOf(t, 1.4, 0.9)
			target, ok := walkableNear(w, spider, 0.5, 64, 0.5)
			if !ok {
				t.Fatal("no place to walk to at the foot of the shelf")
			}
			path, reached := findPath(w, spider, Waypoint{1, 65, 0}, target, pathMaxRange, 0)
			if !reached {
				t.Fatalf("the way to the foot came out as %v, want it reached", path)
			}
			for _, step := range path {
				if step.Z > 0 {
					t.Errorf("the way ran through %v, around the shelf rather than off its edge", step)
				}
			}
		})
	}
}

// TestAPathReplansFromAWalkOffStart stands a wide mob on the cell past a
// shelf's lip, whose own column is air with the lip still under one cell of
// its footprint, and checks its start counts as a place to stand, vanilla's
// WalkNodeEvaluator.canStartAt reading any node that is not OPEN: a goal within
// reach ends on the start with no step, and a blocked goal nearer the start
// than anything the search can open leaves the way empty rather than handing
// the mob a step it did not need.
func TestAPathReplansFromAWalkOffStart(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	spider := strideOf(t, 1.4, 0.9)

	// A plain floor with a one-block lip on it: the cell past the lip has
	// its own column in air, with the lip still under its footprint.
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, stone)
	w.set(1, 64, 0, stone)

	if path, ok := findPath(w, spider, Waypoint{0, 65, 0}, Waypoint{1, 65, 0}, pathMaxRange, 1); !ok || len(path) != 0 {
		t.Errorf("the ask within reach of a walk-off start came back as %v, reached=%v, want no step", path, ok)
	}

	if path, ok := findPath(w, spider, Waypoint{0, 65, 0}, Waypoint{0, 64, 0}, pathMaxRange, 0); ok || len(path) != 0 {
		t.Errorf("the ask to the cell under a walk-off start came back as %v, reached=%v, want no step", path, ok)
	}
}

// TestAZombieDropsThreeButNotFour checks the deepest fall one waypoint may
// take: the no-target Mob.getMaxFallDistance of three, so a pillar top three
// blocks over the floor is a step down to it and one four over is not.
func TestAZombieDropsThreeButNotFour(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	zombie := strideOf(t, 0.6, 1.95)

	// A floor with a one-cell pillar at (3,0) of the given height above it.
	pillar := func(height int32) testWorld {
		w := newTestWorld()
		floor(w, -6, -6, 6, 6, 63, stone)
		for y := int32(64); y < 64+height; y++ {
			w.set(3, y, 0, stone)
		}
		return w
	}

	if _, ok := findPath(pillar(3), zombie, Waypoint{3, 67, 0}, Waypoint{2, 64, 0}, pathMaxRange, 0); !ok {
		t.Error("no way down a three-block drop, want it taken")
	}
	if _, ok := findPath(pillar(4), zombie, Waypoint{3, 68, 0}, Waypoint{2, 64, 0}, pathMaxRange, 0); ok {
		t.Error("a way down a four-block drop was found, want it beyond the fall a walker may take")
	}
}

// TestAHuntingZombieOnHardTakesADeepDrop stands a zombie on a ledge eleven
// blocks over the floor with its target below: on hard at full health its
// allowance is 3 + (20 - 6.6 - 0) = 16, so the path steps off the ledge; on
// easy it is 3 + 13 - 8 = 8, and without a target 3: no way down.
func TestAHuntingZombieOnHardTakesADeepDrop(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	build := func(difficulty uint8) testWorld {
		w := newTestWorld()
		w.diff, w.diffUp = difficulty, true
		floor(w, -25, -25, 25, 25, 59, stone)
		floor(w, -25, -25, 0, 25, 70, stone)
		return w
	}
	zombie := NewMob(TypeZombie, 1, 0.5, 71, 0.5, 1)
	from, to := Waypoint{0, 71, 0}, Waypoint{4, 60, 0}
	reach := func(w testWorld) bool {
		foot := footprintOf(0.6, 1.95)
		foot.maxFall = zombie.maxFallDistance(w)
		_, ok := findPath(w, foot, from, to, pathMaxRange, 0)
		return ok
	}
	zombie.TargetID = 7
	if got := zombie.maxFallDistance(build(3)); got != 16 {
		t.Fatalf("hard allowance = %d, want 16", got)
	}
	if got := zombie.maxFallDistance(build(1)); got != 8 {
		t.Errorf("easy allowance = %d, want 8", got)
	}
	if !reach(build(3)) {
		t.Error("a hunting zombie on hard found no way down an eleven-block ledge")
	}
	if reach(build(1)) {
		t.Error("a hunting zombie on easy walked off an eleven-block ledge")
	}
	zombie.TargetID = 0
	if reach(build(3)) {
		t.Error("an idle zombie walked off an eleven-block ledge")
	}
}

// BenchmarkHunterFlatSearch is a full-range search over open flat ground
// that never reaches its target, at the idle drop allowance and a hunter's.
func BenchmarkHunterFlatSearch(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 59, stone)
	for _, maxFall := range []int32{defaultMaxFall, 12, 64} {
		b.Run(fmt.Sprintf("fall%d", maxFall), func(b *testing.B) {
			foot := footprintOf(0.6, 1.95)
			foot.maxFall = maxFall
			for b.Loop() {
				findPath(w, foot, Waypoint{0, 60, 0}, Waypoint{500, 60, 0}, 16, 0)
			}
		})
	}
}

// TestMaxFallDistanceRoundsInFloat32 pins vanilla's float arithmetic: 100
// health gives 67 beyond the 3, where float64 truncates to 66.
func TestMaxFallDistanceRoundsInFloat32(t *testing.T) {
	w := newTestWorld()
	w.diff, w.diffUp = 3, true
	for _, c := range []struct {
		health float64
		want   int32
	}{{100, 3 + 67}, {200, 3 + 134}, {300, 3 + 201}} {
		m := NewMob(TypeIronGolem, 1, 0, 0, 0, 1)
		m.TargetID, m.Health, m.MaxHealth = 7, c.health, c.health
		if got := m.maxFallDistance(w); got != c.want {
			t.Errorf("health %v: allowance %d, want %d", c.health, got, c.want)
		}
	}
}

// TestMaxFallDistanceByKindAndDifficulty covers the creeper branch and the
// difficulty subtraction at full health: 20 - 6.6 truncates to 13.
func TestMaxFallDistanceByKindAndDifficulty(t *testing.T) {
	for _, c := range []struct {
		kind       MobType
		difficulty uint8
		want       int32
	}{
		{TypeZombie, 0, 3 + 13 - 12},
		{TypeZombie, 1, 3 + 13 - 8},
		{TypeZombie, 2, 3 + 13 - 4},
		{TypeZombie, 3, 3 + 13},
		{TypeCreeper, 0, 3 + 19},
		{TypeCreeper, 3, 3 + 19},
	} {
		w := newTestWorld()
		w.diff, w.diffUp = c.difficulty, true
		m := NewMob(c.kind, 1, 0, 0, 0, 1)
		m.TargetID = 7
		if got := m.maxFallDistance(w); got != c.want {
			t.Errorf("kind %v difficulty %d: allowance %d, want %d", c.kind, c.difficulty, got, c.want)
		}
	}
}

// TestADeepDropNeedsItsColumnOpen roofs the column beside a ledge: the
// landing is reachable only through a roof, so a deep drop is refused while
// the same drop down an open column is taken.
func TestADeepDropNeedsItsColumnOpen(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	run := func(roof bool) bool {
		w := newTestWorld()
		floor(w, -5, -5, 5, 5, 59, stone)
		for y := int32(60); y <= 70; y++ {
			floor(w, -5, -5, 0, 5, y, stone)
		}
		if roof {
			floor(w, 1, -5, 5, 5, 66, stone)
		}
		foot := footprintOf(0.6, 1.95)
		foot.maxFall = 16
		_, ok := findPath(w, foot, Waypoint{0, 71, 0}, Waypoint{3, 60, 0}, pathMaxRange, 0)
		return ok
	}
	if !run(false) {
		t.Error("no way down an open eleven-block column")
	}
	if run(true) {
		t.Error("a deep drop was taken through a roof")
	}
}

// TestAWideMobWalksToTheCentreOfItsFootprintAtAWaypoint gives a spider, whose
// footprint is two cells across, a waypoint and checks it heads for the corner
// between the four cells of that footprint rather than the node cell's centre,
// while a zombie still heads for its cell's centre.
func TestAWideMobWalksToTheCentreOfItsFootprintAtAWaypoint(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for _, tc := range []struct {
		name         string
		kind         MobType
		wantX, wantZ float64
	}{
		{"spider", TypeSpider, 4, 1},
		{"zombie", TypeZombie, 3.5, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -6, -6, 6, 6, 63, stone)
			m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 1)
			m.OnGround = true
			m.nav.path = []Waypoint{{X: 3, Y: 64, Z: 0}}
			m.navigate(w)
			dx, dz := tc.wantX-m.X, tc.wantZ-m.Z
			length := math.Hypot(dx, dz)
			if got := m.walk.dirX; math.Abs(got-dx/length) > 1e-9 {
				t.Errorf("walk dirX = %v, want %v", got, dx/length)
			}
			if got := m.walk.dirZ; math.Abs(got-dz/length) > 1e-9 {
				t.Errorf("walk dirZ = %v, want %v", got, dz/length)
			}
		})
	}
}

// TestAWideMobBanksAWaypointByItsCellCentre stands a spider on the centre of
// the node's cell, which is not the centre of its footprint, and checks the
// waypoint is made; the stuck watch likewise measures to the cell centre.
func TestAWideMobBanksAWaypointByItsCellCentre(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, stone)
	m := NewMob(TypeSpider, 1, 3.5, 64, 0.5, 1)
	m.OnGround = true
	m.nav.path = []Waypoint{{X: 3, Y: 64, Z: 0}, {X: 6, Y: 64, Z: 0}}
	m.navigate(w)
	if len(m.nav.path) != 1 || m.nav.path[0].X != 6 {
		t.Errorf("path = %v, want the first waypoint made", m.nav.path)
	}

	m = NewMob(TypeSpider, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	m.nav.path = []Waypoint{{X: 3, Y: 64, Z: 0}}
	m.nav.speedFactor = 1
	m.stuckStep()
	speed := m.pathSpeed()
	if want := 3 / speed * 20; math.Abs(m.nav.timeoutLimit-want) > 1e-9 {
		t.Errorf("timeoutLimit = %v, want %v", m.nav.timeoutLimit, want)
	}
}

// fireStrip lays a stone floor with a two-wide strip of the block given
// across the way at x = 5 except for a one-wide gap at z = 0, across the floor.
func fireStrip(t *testing.T, strip uint16) testWorld {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -8, 12, 8, 63, stone)
	for z := int32(-8); z <= 8; z++ {
		if z == 0 {
			continue
		}
		w.set(5, 64, z, strip)
	}
	return w
}

// TestAPathWalksThroughTheGapBesideAFireStrip: fire costs 16 a cell, so the
// search takes the gap in the strip over a straight line across it.
// vanilla: PathType.FIRE
func TestAPathWalksThroughTheGapBesideAFireStrip(t *testing.T) {
	w := fireStrip(t, defaultState(t, "minecraft:fire"))
	path, ok := findPath(w, strideOf(t, 0.6, 1.8), Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
	if !ok {
		t.Fatal("no path across the strip, want one through the gap")
	}
	for _, c := range path {
		if rules.IsFire(w.BlockAt(c.X, c.Y, c.Z)) {
			t.Fatalf("path %v steps into fire at %v", path, c)
		}
	}
}

// TestAnAnimalNeverPathsThroughFire: every fire-averse kind built normally
// gets the -1 fire malus from setFireMaluses, so with the gap walled shut it
// finds no way across, while a zombie crosses at a price.
// vanilla: Animal, AbstractVillager constructors
func TestAnAnimalNeverPathsThroughFire(t *testing.T) {
	w := fireStrip(t, defaultState(t, "minecraft:fire"))
	w.set(5, 64, 0, defaultState(t, "minecraft:fire"))
	for _, tc := range []struct {
		kind  MobType
		cross bool
	}{
		{TypeCow, false}, {TypeWolf, false}, {TypeVillager, false}, {TypeZombie, true},
	} {
		m := NewMob(tc.kind, 1, 2.5, 64, 3.5, 1)
		f := footprintOf(m.width(), m.height())
		m.setFireMaluses(&f)
		_, ok := findPath(w, f, Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
		if ok != tc.cross {
			t.Errorf("%s: found a path across the fire = %v, want %v", tc.kind, ok, tc.cross)
		}
	}
}

// TestLavaAndWaterPricesByKind: the wither skeleton, zombified piglin and
// creaking wade a sealed lava strip at 8 a cell where other kinds find no way,
// and a sniffer never paths into water unless it starts the search burning.
// vanilla: WitherSkeleton, ZombifiedPiglin, Creaking, Sniffer constructors
func TestLavaAndWaterPricesByKind(t *testing.T) {
	from, to := Waypoint{2, 64, 3}, Waypoint{8, 64, 3}
	lava := defaultState(t, "minecraft:lava")
	w := fireStrip(t, lava)
	w.set(5, 64, 0, lava)
	for _, tc := range []struct {
		kind  MobType
		cross bool
	}{
		{TypeWitherSkeleton, true}, {TypeZombifiedPiglin, true}, {TypeCreaking, true},
		{TypeZombie, false}, {TypeSkeleton, false},
	} {
		m := NewMob(tc.kind, 1, 2.5, 64, 3.5, 1)
		if _, ok := findPath(w, m.pathFootprint(w), from, to, pathMaxRange*2, 0); ok != tc.cross {
			t.Errorf("%s: found a path across the lava = %v, want %v", tc.kind, ok, tc.cross)
		}
	}
	creaking := NewMob(TypeCreaking, 1, 2.5, 64, 3.5, 1).pathFootprint(w)
	if creaking.fireMalus != 0 || creaking.nearFireMalus != 0 || creaking.lavaMalus != 8 {
		t.Errorf("creaking fire, near fire, lava malus = %v, %v, %v, want 0, 0, 8",
			creaking.fireMalus, creaking.nearFireMalus, creaking.lavaMalus)
	}

	water := defaultState(t, "minecraft:water")
	w = fireStrip(t, water)
	w.set(5, 64, 0, water)
	burning := NewMob(TypeSniffer, 1, 2.5, 64, 3.5, 1)
	burning.FireTicks = 100
	for _, tc := range []struct {
		name  string
		m     *Mob
		malus float64
		cross bool
	}{
		{"sniffer", NewMob(TypeSniffer, 1, 2.5, 64, 3.5, 1), -1, false},
		{"burning sniffer", burning, 0, true},
		{"cow", NewMob(TypeCow, 1, 2.5, 64, 3.5, 1), 8, true},
	} {
		f := tc.m.pathFootprint(w)
		if f.waterMalus != tc.malus {
			t.Errorf("%s: water malus = %v, want %v", tc.name, f.waterMalus, tc.malus)
		}
		if _, ok := findPath(w, f, from, to, pathMaxRange*2, 0); ok != tc.cross {
			t.Errorf("%s: found a path across the water = %v, want %v", tc.name, ok, tc.cross)
		}
	}
}

// TestAPathNeverCrossesARailUnlessTheBodyStandsOnOne: a rail cell is
// impassable except to a body on or over a rail, and to the warden.
// vanilla: PathType.UNPASSABLE_RAIL
func TestAPathNeverCrossesARailUnlessTheBodyStandsOnOne(t *testing.T) {
	rail := defaultState(t, "minecraft:rail")
	w := fireStrip(t, rail)
	from, to := Waypoint{2, 64, 3}, Waypoint{8, 64, 3}
	for _, kind := range []MobType{TypeZombie, TypeCow} {
		m := NewMob(kind, 1, 2.5, 64, 3.5, 1)
		path, ok := findPath(w, m.pathFootprint(w), from, to, pathMaxRange*2, 0)
		if !ok {
			t.Fatalf("%s: no path through the gap", kind)
		}
		for _, c := range path {
			if c.X == 5 && c.Z != 0 {
				t.Fatalf("%s: path %v crosses a rail at %v", kind, path, c)
			}
		}
	}
	w.set(5, 64, 0, rail)
	for _, tc := range []struct {
		m     *Mob
		cross bool
	}{
		{NewMob(TypeZombie, 1, 2.5, 64, 3.5, 1), false},
		{NewMob(TypeCow, 1, 2.5, 64, 3.5, 1), false},
		{newTestWarden(), true},
	} {
		if _, ok := findPath(w, tc.m.pathFootprint(w), from, to, pathMaxRange*2, 0); ok != tc.cross {
			t.Errorf("%s: path across the rails = %v, want %v", tc.m.Type, ok, tc.cross)
		}
	}
	on := NewMob(TypeZombie, 1, 5.5, 64, 3.5, 1)
	foot := on.pathFootprint(w)
	if foot.railMalus != 0 {
		t.Fatalf("railMalus on a rail = %v, want 0", foot.railMalus)
	}
	if _, ok := findPath(w, foot, Waypoint{5, 64, 3}, to, pathMaxRange*2, 0); !ok {
		t.Fatal("a body on a rail finds no path")
	}
	for _, tc := range []struct {
		x     float64
		cross bool
	}{{2.5, false}, {5.5, true}} {
		m := NewMob(TypeZombie, 1, tc.x, 64, 3.5, 1)
		m.OnGround = true
		if _, _, reached := m.createReachingPath(w, 8.5, 64, 3.5); reached != tc.cross {
			t.Errorf("createReachingPath from x=%v across the rails = %v, want %v", tc.x, reached, tc.cross)
		}
	}
}

// TestAPathNeverEntersLava: lava is impassable to the default body, so a
// lava strip with no gap is never entered, and with a gap the path uses it.
// vanilla: PathType.LAVA
func TestAPathNeverEntersLava(t *testing.T) {
	lava := defaultState(t, "minecraft:lava")
	w := fireStrip(t, lava)
	w.set(5, 64, 0, lava)
	path, ok := findPath(w, strideOf(t, 0.6, 1.8), Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
	if ok {
		t.Fatalf("path %v crosses the lava", path)
	}
	for _, c := range path {
		if rules.IsLava(w.BlockAt(c.X, c.Y, c.Z)) {
			t.Fatalf("path %v steps into lava at %v", path, c)
		}
	}
	w.set(5, 64, 0, 0)
	if _, ok := findPath(w, strideOf(t, 0.6, 1.8), Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0); !ok {
		t.Fatal("no path through the gap in the lava")
	}
}

// TestPositionPathTypeClassifiesTheBlockInTheFeetCell pins the types a rail,
// cocoa, powder snow or dripstone in the cell give, and that they keep
// priority over the fire next to them.
// vanilla: WalkNodeEvaluator.getPathTypeFromState
func TestPositionPathTypeClassifiesTheBlockInTheFeetCell(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for _, c := range []struct {
		block string
		want  int
	}{
		{"minecraft:rail", pathRail},
		{"minecraft:detector_rail", pathRail},
		{"minecraft:cocoa", pathCocoa},
		{"minecraft:powder_snow", pathPowderSnow},
		{"minecraft:pointed_dripstone", pathDamageCautious},
		{"minecraft:cactus", pathDamaging},
		{"minecraft:sweet_berry_bush", pathDamaging},
		{"minecraft:wither_rose", pathDamageCautious},
		{"minecraft:air", pathClear},
	} {
		w := newTestWorld()
		w.set(0, 63, 0, stone)
		w.set(0, 64, 0, defaultState(t, c.block))
		if got := positionPathType(w, 0, 64, 0); got != c.want {
			t.Errorf("%s in the feet cell is path type %d, want %d", c.block, got, c.want)
		}
		w.set(1, 64, 0, defaultState(t, "minecraft:fire"))
		if c.want != pathClear {
			if got := positionPathType(w, 0, 64, 0); got != c.want {
				t.Errorf("%s beside fire is path type %d, want %d", c.block, got, c.want)
			}
		}
	}
	// A dripstone beside an open cell on a floor makes it damage-cautious.
	w := newTestWorld()
	w.set(0, 63, 0, stone)
	w.set(1, 64, 0, defaultState(t, "minecraft:pointed_dripstone"))
	if got := positionPathType(w, 0, 64, 0); got != pathDamageCautious {
		t.Errorf("a cell beside dripstone is path type %d, want %d", got, pathDamageCautious)
	}
}

// TestAPathRunsThroughTheCellsOfACarpetFloor lays a lane of carpets, walled
// in on both sides, and checks the path crosses it in the carpets' own cells:
// a carpet is pathfindable, so it neither blocks the body nor floors the cell
// above it. vanilla: BlockBehaviour.isPathfindable
func TestAPathRunsThroughTheCellsOfACarpetFloor(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for _, name := range []string{"minecraft:red_carpet", "minecraft:moss_carpet", "minecraft:repeater", "minecraft:daylight_detector", "minecraft:white_candle", "minecraft:turtle_egg", "minecraft:snow", "minecraft:ladder", "minecraft:scaffolding", "minecraft:pitcher_crop", "minecraft:honey_block"} {
		w := newTestWorld()
		floor(w, -8, -8, 12, 8, 63, stone)
		for x := int32(-8); x <= 12; x++ {
			for _, z := range []int32{-1, 1} {
				w.set(x, 64, z, stone)
				w.set(x, 65, z, stone)
			}
		}
		for x := int32(3); x <= 6; x++ {
			w.set(x, 64, 0, defaultState(t, name))
		}
		path, ok := findPath(w, strideOf(t, 0.6, 1.95), Waypoint{0, 64, 0}, Waypoint{9, 64, 0}, pathMaxRange, 0)
		if !ok {
			t.Fatalf("%s: no path along the lane, want one", name)
		}
		crossed := 0
		for _, step := range path {
			if step.X >= 3 && step.X <= 6 {
				crossed++
				if step.Y != 64 {
					t.Errorf("%s: the path passes %v, want the carpet's own cell at y=64", name, step)
				}
			}
		}
		if crossed == 0 {
			t.Errorf("%s: the path never enters the lane", name)
		}
	}
}

// TestPathfindableOpenKeepsPistonsAndThickSnowSolid checks the blocks that
// override isPathfindable to false stay blocking, and a snow layer is open
// only under five. vanilla: PistonBaseBlock.isPathfindable, SnowLayerBlock.isPathfindable
func TestPathfindableOpenKeepsPistonsAndThickSnowSolid(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		want  bool
	}{
		{"minecraft:piston", map[string]string{"extended": "true", "facing": "north"}, false},
		{"minecraft:sticky_piston", map[string]string{"extended": "true", "facing": "north"}, false},
		{"minecraft:piston", map[string]string{"extended": "false", "facing": "north"}, false},
		{"minecraft:snow", map[string]string{"layers": "1"}, true},
		{"minecraft:snow", map[string]string{"layers": "4"}, true},
		{"minecraft:snow", map[string]string{"layers": "5"}, false},
		{"minecraft:stone", nil, false},
		{"minecraft:red_carpet", nil, true},
		{"minecraft:lily_pad", nil, false},
		{"minecraft:big_dripleaf", nil, false},
		{"minecraft:ladder", map[string]string{"facing": "north"}, true},
	}
	for _, c := range cases {
		if got := pathfindableOpen(registry.MustState(c.name, c.props)); got != c.want {
			t.Errorf("pathfindableOpen(%s %v) = %v, want %v", c.name, c.props, got, c.want)
		}
	}
}

// TestABlockPlacedOnAStandingPathReplansIt walls a standing path off: with
// more than 20 ticks since the plan the path is replanned at once, with fewer
// the old one stands until the delayed recompute runs.
// vanilla: PathNavigation.shouldRecomputePath, recomputePath
func TestABlockPlacedOnAStandingPathReplansIt(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	setup := func(ticks int) (testWorld, *Mob) {
		w := newTestWorld()
		floor(w, -40, -40, 40, 40, 63, stone)
		z := aimless(NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1))
		z.Tick(w, nil)
		z.seekTo(w, 12.5, 64, 0.5, 1.0, 0)
		for range ticks {
			z.Tick(w, nil)
		}
		wall(w, stone, 4, -40, 40, 10, 10, 64)
		Mobs{z}.BlocksChanged(w, []Waypoint{{4, 64, 0}})
		return w, z
	}
	throughWall := func(z *Mob) bool {
		for _, p := range z.nav.path {
			if p.X == 4 && p.Z != 10 {
				return true
			}
		}
		return false
	}
	t.Run("replans at once", func(t *testing.T) {
		_, z := setup(25)
		if len(z.nav.path) == 0 || throughWall(z) {
			t.Fatalf("path %v is empty or runs through the wall", z.nav.path)
		}
	})
	t.Run("replans after the delay", func(t *testing.T) {
		w, z := setup(5)
		if !throughWall(z) || !z.nav.delayedRecompute {
			t.Fatalf("old path should stand with a delayed recompute: %v, %v", z.nav.path, z.nav.delayedRecompute)
		}
		for range 20 {
			z.Tick(w, nil)
		}
		if len(z.nav.path) == 0 || throughWall(z) {
			t.Fatalf("path %v is empty or runs through the wall", z.nav.path)
		}
	})
}

// TestAPathKeepsOutOfPowderSnowBerryBushesAndCactus: a sealed strip of each
// block is not crossed, except by the kinds that price it at or above zero.
// vanilla: PathType DAMAGING, POWDER_SNOW, DAMAGING_IN_NEIGHBOR
func TestAPathKeepsOutOfPowderSnowBerryBushesAndCactus(t *testing.T) {
	cases := []struct {
		block                string
		zombie, cow, fox, wd bool
	}{
		{"minecraft:powder_snow", false, false, false, true},
		{"minecraft:sweet_berry_bush", false, false, true, true},
		{"minecraft:cactus", false, false, true, true},
	}
	for _, c := range cases {
		state := defaultState(t, c.block)
		w := fireStrip(t, state)
		w.set(5, 64, 0, state)
		for _, k := range []struct {
			name string
			m    *Mob
			want bool
		}{
			{"zombie", NewMob(TypeZombie, 1, 2.5, 64, 3.5, 1), c.zombie},
			{"cow", NewMob(TypeCow, 1, 2.5, 64, 3.5, 1), c.cow},
			{"fox", NewMob(TypeFox, 1, 2.5, 64, 3.5, 1), c.fox},
			{"warden", newTestWarden(), c.wd},
		} {
			if k.name == "warden" {
				k.m.X, k.m.Y, k.m.Z = 2.5, 64, 3.5
			}
			_, ok := findPath(w, k.m.pathFootprint(w), Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
			if ok != k.want {
				t.Errorf("%s across %s: ok=%v, want %v", k.name, c.block, ok, k.want)
			}
		}
	}

	bush := defaultState(t, "minecraft:sweet_berry_bush")
	w := fireStrip(t, bush)
	zombie := NewMob(TypeZombie, 1, 2.5, 64, 3.5, 1)
	foot := zombie.pathFootprint(w)
	path, ok := findPath(w, foot, Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
	if !ok {
		t.Fatal("no path through the gap beside the bushes")
	}
	for _, c := range path {
		if c.X == 5 && c.Z != 0 {
			t.Fatalf("path %v crosses the bushes at %v", path, c)
		}
	}
	if got := cellMalus(w, foot, 5, 64, 0); got != 8 {
		t.Errorf("gap cell malus = %v, want 8", got)
	}

	stone := defaultState(t, "minecraft:stone")
	cw := newTestWorld()
	cw.set(0, 63, 0, stone)
	cw.set(1, 64, 0, defaultState(t, "minecraft:cactus"))
	if got := positionPathType(cw, 0, 64, 0); got != pathNearDamaging {
		t.Errorf("cell beside a cactus is path type %d, want %d", got, pathNearDamaging)
	}
	cw.set(-1, 64, 0, defaultState(t, "minecraft:wither_rose"))
	if got := positionPathType(cw, 0, 64, 0); got != pathDamageCautious {
		t.Errorf("cell beside a wither rose and a cactus is path type %d, want %d", got, pathDamageCautious)
	}
	if got := positionPathType(cw, 1, 65, 0); got != pathDamaging {
		t.Errorf("cell on a cactus is path type %d, want %d", got, pathDamaging)
	}
}

// TestGoatsWolvesAndSniffersKeepOffPowderSnow: an open cell over powder snow
// sunk into the floor is ON_TOP_OF_POWDER_SNOW, which the goat, wolf and
// sniffer price at -1, so a sealed strip of it turns them back while a cow
// and a zombie cross.
// vanilla: WalkNodeEvaluator.getPathTypeStatic, Goat.<init>
func TestGoatsWolvesAndSniffersKeepOffPowderSnow(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	snow := defaultState(t, "minecraft:powder_snow")
	w := newTestWorld()
	floor(w, -2, -8, 12, 8, 63, stone)
	for z := int32(-8); z <= 8; z++ {
		w.set(5, 63, z, snow)
	}
	if got := positionPathType(w, 5, 64, 0); got != pathOnTopOfPowderSnow {
		t.Fatalf("cell over powder snow is path type %d, want %d", got, pathOnTopOfPowderSnow)
	}
	for _, k := range []struct {
		typ   MobType
		cross bool
	}{
		{TypeGoat, false}, {TypeWolf, false}, {TypeSniffer, false},
		{TypeCow, true}, {TypeZombie, true},
	} {
		m := NewMob(k.typ, 1, 2.5, 64, 3.5, 1)
		foot := m.pathFootprint(w)
		want := 0.0
		if !k.cross {
			want = -1
		}
		if got := foot.malusOfType(pathOnTopOfPowderSnow); got != want {
			t.Errorf("%v prices a cell over powder snow at %v, want %v", k.typ, got, want)
		}
		_, ok := findPath(w, foot, Waypoint{2, 64, 3}, Waypoint{8, 64, 3}, pathMaxRange*2, 0)
		if ok != k.cross {
			t.Errorf("%v across sunk powder snow: ok=%v, want %v", k.typ, ok, k.cross)
		}
	}
}

// TestAStepUpBesideASolidBlockClimbsOverPowderSnow: a level cell the body
// cannot enter makes the mob's path type BLOCKED, ahead of POWDER_SNOW, so the
// jump onto the step is still tried; powder snow alone is never jumped onto.
// vanilla: WalkNodeEvaluator.findAcceptedNode, getPathTypeOfMob
func TestAStepUpBesideASolidBlockClimbsOverPowderSnow(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	snow := defaultState(t, "minecraft:powder_snow")
	from := Waypoint{5, 64, 0}
	up := pathStep{1, 1, 0, 1}
	for _, c := range []struct {
		name  string
		typ   MobType
		place map[Waypoint]uint16
		want  bool
	}{
		{"warden onto snow over a stone step", TypeWarden,
			map[Waypoint]uint16{{6, 64, 0}: stone, {6, 65, 0}: snow}, true},
		{"iron golem onto a stone and snow step", TypeIronGolem,
			map[Waypoint]uint16{{6, 64, 0}: stone, {6, 64, 1}: snow}, true},
		{"cow onto a block of powder snow", TypeCow,
			map[Waypoint]uint16{{6, 64, 0}: snow}, false},
	} {
		w := newTestWorld()
		floor(w, 0, -4, 10, 4, 63, stone)
		for p, s := range c.place {
			w.set(p.X, p.Y, p.Z, s)
		}
		foot := NewMob(c.typ, 1, 5.5, 64, 0.5, 1).pathFootprint(w)
		if got := stepTaken(w, foot, from, up); got != c.want {
			t.Errorf("%s: stepTaken = %v, want %v", c.name, got, c.want)
		}
	}
}

// axolotlStride is an amphibious one-cell body, the axolotl's.
func axolotlStride(t *testing.T) footprint {
	t.Helper()
	f := strideOf(t, 0.75, 0.42)
	f.amphibious = true
	return f
}

// TestAnAmphibiousCellIsPricedByWaterAndLand holds AmphibiousNodeEvaluator's
// prices: 6 for floored land, 4 for water against a blocked face, 0 for
// open water and 0 over a lily pad or big dripleaf.
func TestAnAmphibiousCellIsPricedByWaterAndLand(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	foot := axolotlStride(t)

	w.set(0, 63, 0, stone)
	if got := cellMalus(w, foot, 0, 64, 0); got != 6 {
		t.Errorf("a dry cell over stone costs %v, want 6", got)
	}

	w.set(10, 63, 0, stone)
	w.set(10, 64, 0, water)
	if got := cellMalus(w, foot, 10, 64, 0); got != 4 {
		t.Errorf("a water cell over a stone floor costs %v, want 4", got)
	}

	for x := int32(19); x <= 21; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(-1); z <= 1; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	if got := cellMalus(w, foot, 20, 64, 0); got != 0 {
		t.Errorf("a water cell with water on all six sides costs %v, want 0", got)
	}

	for i, name := range []string{"minecraft:lily_pad", "minecraft:big_dripleaf"} {
		x := int32(30 + 10*i)
		w.set(x, 62, 0, water)
		w.set(x, 63, 0, defaultState(t, name))
		if got := cellMalus(w, foot, x, 64, 0); got != 0 {
			t.Errorf("a cell over %s costs %v, want 0", name, got)
		}
	}

	w.set(60, 63, 0, stone)
	w.set(61, 63, 0, water)
	if got := cellMalus(w, foot, 60, 64, 0); got != 4 {
		t.Errorf("a dry cell beside water costs %v, want WATER_BORDER's 4", got)
	}
}

// TestWaterCellsArePricedByThePathType holds PathType WATER and WATER_BORDER's
// 8 for a walker, and the 0 a water-going kind and a following pet plan with.
func TestWaterCellsArePricedByThePathType(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	floor(w, -1, -1, 3, 1, 63, stone)
	w.set(0, 63, 0, water)
	w.set(0, 64, 0, water)
	walker := strideOf(t, 0.6, 1.95)
	if got := cellMalus(w, walker, 0, 64, 0); got != 8 {
		t.Errorf("a wading cell costs %v, want WATER's 8", got)
	}
	if got := cellMalus(w, walker, 1, 64, 0); got != 8 {
		t.Errorf("a land cell beside water costs %v, want WATER_BORDER's 8", got)
	}
	if got := cellMalus(w, walker, 3, 64, 0); got != 0 {
		t.Errorf("a land cell away from water costs %v, want 0", got)
	}
	// A waterlogged ladder is pathfindable, so it reads as WATER; a waterlogged
	// rail reads as RAIL first.
	for _, c := range []struct {
		name string
		want float64
	}{{"minecraft:ladder", 8}, {"minecraft:rail", 0}} {
		logged, ok := registry.WithBool(defaultState(t, c.name), registry.PropWaterlogged, true)
		if !ok {
			t.Fatalf("%s has no waterlogged state", c.name)
		}
		lw := newTestWorld()
		floor(lw, -1, -1, 1, 1, 63, stone)
		lw.set(0, 64, 1, logged)
		if got := cellMalus(lw, walker, 0, 64, 0); got != c.want {
			t.Errorf("a land cell beside a waterlogged %s costs %v, want %v", c.name, got, c.want)
		}
	}

	chicken := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 1).pathFootprint(w)
	if got := cellMalus(w, chicken, 0, 64, 0); got != 0 {
		t.Errorf("a chicken's wading cell costs %v, want 0", got)
	}
	wolf := NewMob(TypeWolf, 2, 0.5, 64, 0.5, 1)
	if got := cellMalus(w, wolf.pathFootprint(w), 0, 64, 0); got != 8 {
		t.Errorf("an idle wolf's wading cell costs %v, want 8", got)
	}
	follow := &followOwnerGoal{m: wolf}
	follow.Start(nil)
	if got := cellMalus(w, wolf.pathFootprint(w), 0, 64, 0); got != 0 {
		t.Errorf("a wolf following its owner prices a wading cell at %v, want 0", got)
	}
	follow.Stop(nil)
	if got := cellMalus(w, wolf.pathFootprint(w), 0, 64, 0); got != 8 {
		t.Errorf("a wolf done following prices a wading cell at %v, want 8", got)
	}
}

// TestAnAmphibiousPathSwimsOffThePoolFloor lays a pool four deep: the floor
// cells pay the water border's 4, so the path rises into open water.
func TestAnAmphibiousPathSwimsOffThePoolFloor(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	for y := int32(56); y <= 63; y++ {
		floor(w, -2, -2, 8, 8, y, stone)
	}
	for y := int32(60); y <= 63; y++ {
		floor(w, 0, 0, 6, 6, y, water)
	}

	path, reached := findPath(w, axolotlStride(t), Waypoint{0, 60, 0}, Waypoint{6, 60, 6}, pathMaxRange, 0)
	if !reached {
		t.Fatal("found no way across the pool")
	}
	onFloor := 0
	for _, p := range path {
		if p.Y == 60 {
			onFloor++
		}
	}
	if onFloor*2 >= len(path) {
		t.Errorf("%d of %d nodes lie on the pool floor, want fewer than half: %v", onFloor, len(path), path)
	}
}

// vanilla: WalkNodeEvaluator.getMobJumpHeight, findAcceptedNode
func TestAPathStepsOntoAFenceOnlyWithPermission(t *testing.T) {
	for _, name := range []string{"oak_fence", "cobblestone_wall", "oak_fence_gate"} {
		for _, overFences := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/overFences=%v", name, overFences), func(t *testing.T) {
				w := newTestWorld()
				floor(w, -25, -25, 25, 25, 63, defaultState(t, "minecraft:stone"))
				for z := int32(-25); z <= 25; z++ {
					w.set(5, 64, z, defaultState(t, "minecraft:"+name))
				}
				foot := footprintOf(1.7, 2.375)
				foot.jumpHeight, foot.overFences = 1.5, overFences
				path, reached := findPath(w, foot, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, pathMaxRange, 0)
				if reached != overFences {
					t.Fatalf("path %v reached=%v, want %v", path, reached, overFences)
				}
				onBarrier := false
				for _, step := range path {
					onBarrier = onBarrier || step.X == 5 && step.Y == 65
					if !traversableAt(w, foot, step.X, step.Y, step.Z) {
						t.Fatalf("path enters an unwalkable cell: %v", step)
					}
				}
				if onBarrier != overFences {
					t.Errorf("path %v steps onto barrier=%v, want %v", path, onBarrier, overFences)
				}
				if !reached && (len(path) == 0 || path[len(path)-1] != (Waypoint{3, 64, 0})) {
					t.Errorf("path %v, want the two-cell body held at the barrier's foot", path)
				}
			})
		}
	}
}

// vanilla: WalkNodeEvaluator.getMobJumpHeight, Camel constructor
func TestPathFootprintUsesMobStepHeight(t *testing.T) {
	w := newTestWorld()
	for _, typ := range []MobType{TypeCow, TypeCamel, TypeCamelHusk, TypeZombie} {
		m := NewMob(typ, 1, 2.5, 64, 0.5, 1)
		f := m.pathFootprint(w)
		if f.jumpHeight != max(mobJumpHeight, m.stepHeight()) || f.overFences != IsCamel(typ) {
			t.Errorf("%s: jump height %v, overFences %v", typ, f.jumpHeight, f.overFences)
		}
		for _, height := range []float64{0.6, 1.5, 2} {
			a := m.BuildAttributes(nbt.List{})
			a.SetBase(registry.AttributeStepHeight, height)
			m.SetAttributes(a)
			if f := m.pathFootprint(w); f.jumpHeight != max(mobJumpHeight, height) || f.overFences != IsCamel(typ) {
				t.Errorf("%s with step_height %v: jump height %v, overFences %v", typ, height, f.jumpHeight, f.overFences)
			}
		}
	}
}

// A 1.5-block rise onto stone needs height, but no fence permission.
func TestAPathClimbsALedgeWithRaisedStepHeight(t *testing.T) {
	w := newTestWorld()
	floor(w, -25, -25, 25, 25, 64, slabState(t))
	for z := int32(-25); z <= 25; z++ {
		w.set(5, 65, z, defaultState(t, "minecraft:stone"))
	}
	for _, height := range []float64{mobJumpHeight, 1.5} {
		foot := footprintOf(0.6, 1.95)
		foot.jumpHeight = height
		path, reached := findPath(w, foot, Waypoint{2, 65, 0}, Waypoint{8, 65, 0}, pathMaxRange, 0)
		if reached != (height == 1.5) {
			t.Errorf("jump height %v: path %v reached=%v", height, path, reached)
		}
	}
}

func TestBlockPathTypeOrdersWaterloggedStates(t *testing.T) {
	for _, tc := range []struct {
		block registry.Block
		want  int
	}{
		{registry.BlockPointedDripstone, pathDamageCautious},
		{registry.BlockSulfurSpike, pathDamageCautious},
		{registry.BlockOakStairs, pathBlockedType},
		{registry.BlockOakSlab, pathBlockedType},
		{registry.BlockOakFence, pathFence},
		{registry.BlockCobblestoneWall, pathFence},
		{registry.BlockOakLeaves, pathLeaves},
		{registry.BlockMangroveLeaves, pathLeaves},
		{registry.BlockLadder, pathWater},
		{registry.BlockLantern, pathBlockedType},
		{registry.BlockIronChain, pathBlockedType},
		{registry.BlockRail, pathRail},
		{registry.BlockOakTrapdoor, pathTrapdoor},
	} {
		t.Run(tc.block.Name(), func(t *testing.T) {
			state, ok := registry.WithBool(tc.block.DefaultState(), registry.PropWaterlogged, true)
			if !ok {
				t.Fatal("missing waterlogged state")
			}
			if got := blockPathType(state); got != tc.want {
				t.Errorf("path type = %d, want %d", got, tc.want)
			}
			if got := pathBlocked(state); got != (tc.want == pathBlockedType) {
				t.Errorf("blocked = %v, want %v", got, tc.want == pathBlockedType)
			}
			w := newTestWorld()
			w.set(0, 63, 0, registry.BlockStone.DefaultState())
			w.set(1, 64, 0, state)
			want := pathClear
			switch tc.want {
			case pathWater:
				want = pathWaterBorder
			case pathDamageCautious:
				want = pathDamageCautious
			}
			if got := positionPathType(w, 0, 64, 0); got != want {
				t.Errorf("neighbour path type = %d, want %d", got, want)
			}
		})
	}
}
