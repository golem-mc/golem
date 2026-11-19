package entity

import (
	"fmt"
	"testing"
)

// The pathfinding as ordinary unit tests: the A* itself against worlds of
// the blocks each case lays. The walks found are followed in the physics
// tests of mob_test.go; these check the ways the search finds and the ways
// it honestly fails.

// pathMaxRange is the Chebyshev distance, in blocks, that the tests' direct
// findPath calls let a path wander from its first cell.
const pathMaxRange = 20

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

// axolotlStride is an amphibious one-cell body, the axolotl's.
func axolotlStride(t *testing.T) footprint {
	t.Helper()
	f := strideOf(t, 0.75, 0.42)
	f.amphibious = true
	return f
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
