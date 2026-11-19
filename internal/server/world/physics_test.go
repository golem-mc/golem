package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// newPhysicsWorld loads a flat world with the chunks around the origin held,
// the way a server with players there keeps them, so tests can flood, dig and
// build inside loaded chunks — physics anywhere else is deliberately inert.
// The ground is stone up to y=63 and air from GroundLevel up, which is what
// the tests' levels are laid out against.
func newPhysicsWorld(t *testing.T) (*World, *Physics) {
	t.Helper()
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	return w, NewPhysics(w)
}

// setBlock changes a block the way the server's changeBlock does — through
// the world, then through physics' neighbour update — failing if the world
// did not take it, and returns what the neighbour update changed.
func setBlock(t *testing.T, w *World, p *Physics, x, y, z int32, state uint16) []BlockChange {
	t.Helper()
	old := w.BlockAt(x, y, z)
	if !w.SetBlock(x, y, z, state) {
		t.Fatalf("setup: SetBlock(%d, %d, %d, %d) did not take", x, y, z, state)
	}
	changes, _, _ := p.NotifyChanged(x, y, z, old)
	return changes
}

// tickToQuiet ticks physics until more than a lava wave passes with nothing
// changing, and fails if the world is still changing at the limit. Fluids
// settle in waves — water's five ticks apart, lava's thirty — so a quiet
// window has to outlast the longer of the two before it means settled; a
// world still changing after the limit is one physics never finished with.
// The falls a tick hands the server as falling entities do not count against
// the quiet: they are the world's own doing, not a change still pending.
func tickToQuiet(t *testing.T, p *Physics, clock *int64, limit int64) {
	t.Helper()
	const quietWindow = 31
	quiet := 0
	for *clock < limit {
		*clock++
		changes, _, _, _ := p.Tick(*clock)
		if len(changes) > 0 {
			quiet = 0
			continue
		}
		quiet++
		if quiet == quietWindow {
			return
		}
	}
	t.Fatalf("physics still changing after %d ticks", limit)
}

// assertFluid checks that a position holds exactly the given fluid at the
// given level — a source (0), a flowing wedge (1-7), or a falling column (8).
func assertFluid(t *testing.T, w *World, x, y, z int32, kind fluidKind, level int, label string) {
	t.Helper()
	state := w.BlockAt(x, y, z)
	if state != fluidStateAt(kind, level) {
		name := "lava"
		if kind == fluidWater {
			name = "water"
		}
		t.Errorf("%s: block at (%d, %d, %d) = %d, want %s level %d (state %d)",
			label, x, y, z, state, name, level, fluidStateAt(kind, level))
	}
}

// assertAir checks that nothing has spread to a position.
func assertAir(t *testing.T, w *World, x, y, z int32, label string) {
	t.Helper()
	if got := w.BlockAt(x, y, z); got != airState {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("%s: block at (%d, %d, %d) = %s %q, want air", label, x, y, z, name, props)
	}
}

// TestWaterSpreadsOutAndThinsToALevelSevenEdge checks the flat spread: a
// source floods one step weaker for every block it travels, out to seven,
// and nothing at all past that — and never upwards.
func TestWaterSpreadsOutAndThinsToALevelSevenEdge(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 250)

	for d := 1; d <= 7; d++ {
		assertFluid(t, w, int32(d), GroundLevel, 0, fluidWater, d, "east spread")
		assertFluid(t, w, 0, GroundLevel, int32(d), fluidWater, d, "south spread")
	}
	// The eighth block out is dry, the diamond's corners cut before its
	// points do — eight away diagonally — and nothing rose above the
	// source's level.
	assertAir(t, w, 8, GroundLevel, 0, "past the edge")
	assertAir(t, w, 4, GroundLevel, 4, "past the diamond corner")
	assertAir(t, w, 0, GroundLevel+1, 0, "above the source")
	assertFluid(t, w, 0, GroundLevel, 0, fluidWater, 0, "the source itself")
}

// TestWaterFindsItsWayDownAPit checks that the flow is steered: with a drop
// within the slope search's four blocks, the water runs to it instead of
// flooding everywhere, pours into it as a falling column, and does not run
// on past the hole.
func TestWaterFindsItsWayDownAPit(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	// A pit three deep, three blocks east of where the source will be.
	for y := int32(GroundLevel - 3); y < GroundLevel; y++ {
		setBlock(t, w, p, 3, y, 0, airState)
	}
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 250)

	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "the path to the pit")
	assertFluid(t, w, 2, GroundLevel, 0, fluidWater, 2, "the path to the pit")
	assertFluid(t, w, 3, GroundLevel-2, 0, fluidWater, fallingLevel, "the fall")
	assertFluid(t, w, 3, GroundLevel-3, 0, fluidWater, fallingLevel, "the pit floor")
	// Nothing spread away from the path, nothing ran on past the hole, and
	// the pit floor is still stone under the falling column.
	assertAir(t, w, -1, GroundLevel, 0, "behind the source")
	assertAir(t, w, 0, GroundLevel, 1, "beside the source")
	assertAir(t, w, 4, GroundLevel, 0, "past the pit")
	if got := w.BlockAt(3, GroundLevel-4, 0); got != stoneState {
		t.Errorf("block under the pit = %d, want %d (stone)", got, stoneState)
	}
}

// TestWaterRecedingWhenItsSourceIsRemoved checks that a spread slope dries
// back up when what fed it is gone, thinning out to its edges in waves.
func TestWaterRecedingWhenItsSourceIsRemoved(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 250)
	assertFluid(t, w, 4, GroundLevel, 0, fluidWater, 4, "flooded before the source goes")

	setBlock(t, w, p, 0, GroundLevel, 0, airState)
	tickToQuiet(t, p, &clock, 500)

	for d := int32(1); d <= 8; d++ {
		assertAir(t, w, d, GroundLevel, 0, "dried flow")
		assertAir(t, w, 0, GroundLevel, d, "dried flow")
	}
	assertAir(t, w, 3, GroundLevel, 3, "dried flow")
}

// TestTwoSourcesMakeAnInfinitePool checks the diagonal two-by-two: a flowing
// block between two sources over solid ground becomes a source itself, so
// scooping water out of the pool never drains it.
func TestTwoSourcesMakeAnInfinitePool(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	setBlock(t, w, p, 1, GroundLevel, 1, waterByLevel[0])
	tickToQuiet(t, p, &clock, 400)

	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 0, "the pool's third corner")
	assertFluid(t, w, 0, GroundLevel, 1, fluidWater, 0, "the pool's fourth corner")
}

// diagonalPool places two diagonal sources and runs the physics, returning
// whether the pool's third corner became a source.
func diagonalPool(t *testing.T, rule string, value bool, source uint16) bool {
	t.Helper()
	w, p := newPhysicsWorld(t)
	w.SetBoolRule(RuleIndex(rule), value)
	var clock int64
	setBlock(t, w, p, 0, GroundLevel, 0, source)
	setBlock(t, w, p, 1, GroundLevel, 1, source)
	tickToQuiet(t, p, &clock, 1200)
	return fluidHolding(w.BlockAt(1, GroundLevel, 0)).isSource()
}

// TestSourceConversionRules checks both conversion gamerules in both settings.
func TestSourceConversionRules(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		value      bool
		source     uint16
	}{
		{"water on", "water_source_conversion", true, waterByLevel[0]},
		{"water off", "water_source_conversion", false, waterByLevel[0]},
		{"lava on", "lava_source_conversion", true, lavaByLevel[0]},
		{"lava off", "lava_source_conversion", false, lavaByLevel[0]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := diagonalPool(t, tc.rule, tc.value, tc.source); got != tc.value {
				t.Errorf("corner is a source = %v, want %v", got, tc.value)
			}
		})
	}
}

// TestLavaSpreadsThreeBlocksAndStops checks lava's slower, steeper flow: one
// tick every thirty, a level that steps by two, and three blocks of reach.
func TestLavaSpreadsThreeBlocksAndStops(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
	tickToQuiet(t, p, &clock, 800)

	assertFluid(t, w, 1, GroundLevel, 0, fluidLava, 2, "lava spread")
	assertFluid(t, w, 2, GroundLevel, 0, fluidLava, 4, "lava spread")
	assertFluid(t, w, 3, GroundLevel, 0, fluidLava, 6, "lava spread")
	assertAir(t, w, 4, GroundLevel, 0, "lava's reach ends")
	assertAir(t, w, 0, GroundLevel+1, 0, "lava flows down, not up")
}

// TestWaterOnALavaSourceBecomesObsidian checks the first of the two ways the
// fluids meet: water reaching a source of lava turns it to obsidian, and the
// water does not take the block.
func TestWaterOnALavaSourceBecomesObsidian(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 2, GroundLevel, 0, lavaByLevel[0])
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 300)

	if got := w.BlockAt(2, GroundLevel, 0); got != obsidianState {
		t.Errorf("lava source touched by water = %d, want %d (obsidian)", got, obsidianState)
	}
	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "water short of the lava")
}

// TestWaterOnFlowingLavaBecomesCobblestone checks the second way the fluids
// meet: water reaching a flow of lava turns it to cobblestone instead.
func TestWaterOnFlowingLavaBecomesCobblestone(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 2, GroundLevel, 0, lavaByLevel[2])
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 300)

	if got := w.BlockAt(2, GroundLevel, 0); got != cobblestoneState {
		t.Errorf("flowing lava touched by water = %d, want %d (cobblestone)", got, cobblestoneState)
	}
}

// TestLavaPouringOntoWaterBecomesStone checks the third way the fluids meet:
// lava that pours down onto water turns the water to stone, which is why a
// lava cascade over a pool builds a floor of stone under itself.
func TestLavaPouringOntoWaterBecomesStone(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	setBlock(t, w, p, 0, GroundLevel+2, 0, lavaByLevel[0])
	tickToQuiet(t, p, &clock, 600)

	if got := w.BlockAt(0, GroundLevel, 0); got != stoneState {
		t.Errorf("water under pouring lava = %d, want %d (stone)", got, stoneState)
	}
	// The column above survived: the source still pours, and the block
	// above the new stone is still falling lava holding it up.
	assertFluid(t, w, 0, GroundLevel+2, 0, fluidLava, 0, "the lava source")
	assertFluid(t, w, 0, GroundLevel+1, 0, fluidLava, fallingLevel, "the column")
}

// TestWaterWashesAPlantAwayAndDropsIt checks what a spread leaves behind
// when it takes a block: the grass goes, the water stands where it stood,
// and the destruction rides back to the server as a break it rolls loot for.
func TestWaterWashesAPlantAwayAndDropsIt(t *testing.T) {
	w, p := newPhysicsWorld(t)
	grass := registry.BlockStates["minecraft:short_grass"]

	for x := int32(-1); x <= 3; x++ {
		setBlock(t, w, p, x, GroundLevel, -1, stoneState)
		setBlock(t, w, p, x, GroundLevel, 1, stoneState)
	}
	setBlock(t, w, p, 3, GroundLevel, 0, stoneState)
	// Short grass needs its soil, or the flow's neighbour update pops it
	// before the water reaches it.
	setBlock(t, w, p, 1, GroundLevel-1, 0, registry.BlockStates["minecraft:dirt"])
	setBlock(t, w, p, 1, GroundLevel, 0, grass)
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])

	var clock int64
	var dropped *Effect
	for clock < 200 {
		clock++
		_, _, _, effects := p.Tick(clock)
		for i := range effects {
			if effects[i].BreakDrop == grass {
				dropped = &effects[i]
			}
		}
	}
	if dropped == nil {
		t.Fatal("the wash played no break for the grass")
	}
	if dropped.X != 1 || dropped.Y != GroundLevel || dropped.Z != 0 {
		t.Errorf("the break played at (%d, %d, %d), want the grass's own cell",
			dropped.X, dropped.Y, dropped.Z)
	}
	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "the water in the grass's place")
}

// TestSandFallsAsAnEntity checks the hand-off: sand left over air is handed
// to the server as a falling entity after the two ticks the vanilla server
// schedules a fall for, its own cell left as air — the engine moving no
// block itself, the descent being the entity's half of the work.
func TestSandFallsAsAnEntity(t *testing.T) {
	w, p := newPhysicsWorld(t)
	sand := registry.BlockStates["minecraft:sand"]
	var clock int64

	// Sand high above the ground.
	setBlock(t, w, p, 0, GroundLevel+6, 0, sand)

	clock++
	var falls []FallingSpawn
	_, falls, _, _ = p.Tick(clock)
	if got := w.BlockAt(0, GroundLevel+6, 0); got != sand {
		t.Fatalf("sand moved before its two-tick delay: state = %d, want sand", got)
	}
	if len(falls) != 0 {
		t.Fatalf("sand handed over before its two-tick delay: %d falls", len(falls))
	}

	// The hand-off: the sand's old cell becomes air in the same change set
	// the spawn arrives beside, the block carried whole and waterless.
	clock++
	changes, falls, _, _ := p.Tick(clock)
	if got := w.BlockAt(0, GroundLevel+6, 0); got != airState {
		t.Fatalf("block the sand left = %d, want %d (air)", got, airState)
	}
	if findChange(t, changes, 0, GroundLevel+6, 0) != airState {
		t.Error("fall did not report the vacated cell as air")
	}
	if len(falls) != 1 {
		t.Fatalf("the tick handed over %d falls, want 1", len(falls))
	}
	if falls[0].X != 0 || falls[0].Y != GroundLevel+6 || falls[0].Z != 0 {
		t.Errorf("fall handed over at (%d, %d, %d), want (0, %d, 0)",
			falls[0].X, falls[0].Y, falls[0].Z, GroundLevel+6)
	}
	if falls[0].State != sand {
		t.Errorf("fall carries state %d, want %d (sand)", falls[0].State, sand)
	}

	// The engine's part is done: nothing more comes of the fall, the
	// world's blocks having been left exactly as the hand-off left them.
	clock++
	_, falls, _, _ = p.Tick(clock)
	if len(falls) != 0 {
		t.Errorf("the fall handed over a second time: %d falls", len(falls))
	}
	if got := w.BlockAt(0, GroundLevel+6, 0); got != airState {
		t.Errorf("cell the engine went back into = %d, want %d (air)", got, airState)
	}
}

// TestATorchBreaksWithoutSupport checks the immediate reaction: a torch dug
// out from under goes at once, in the neighbour update's own change set
// rather than on any schedule.
func TestATorchBreaksWithoutSupport(t *testing.T) {
	w, p := newPhysicsWorld(t)
	torch := registry.BlockStates["minecraft:torch"]

	// Placing the torch on the ground breaks nothing.
	if changes := setBlock(t, w, p, 0, GroundLevel, 0, torch); len(changes) != 0 {
		t.Fatalf("placing a supported torch changed %d blocks, want 0", len(changes))
	}

	// Removing the ground under it breaks it, immediately.
	changes := setBlock(t, w, p, 0, GroundLevel-1, 0, airState)
	if findChange(t, changes, 0, GroundLevel, 0) != airState {
		t.Errorf("removing the support reported the torch as kept: changes = %v", changes)
	}
	if got := w.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("torch without support = %d, want %d (air)", got, airState)
	}
}

// TestFloorPlantsBreakWithoutSupport checks the wider floor-standing set the
// same way: the grass is named by the engine, and the flower comes in with
// the flowers tag.
func TestFloorPlantsBreakWithoutSupport(t *testing.T) {
	w, p := newPhysicsWorld(t)
	grass := registry.BlockStates["minecraft:short_grass"]
	flower := registry.BlockStates["minecraft:dandelion"]

	setBlock(t, w, p, 0, GroundLevel, 0, grass)
	setBlock(t, w, p, 2, GroundLevel, 0, flower)

	changes := setBlock(t, w, p, 0, GroundLevel-1, 0, airState)
	if findChange(t, changes, 0, GroundLevel, 0) != airState {
		t.Errorf("removing the ground reported the grass as kept: changes = %v", changes)
	}
	changes = setBlock(t, w, p, 2, GroundLevel-1, 0, airState)
	if findChange(t, changes, 2, GroundLevel, 0) != airState {
		t.Errorf("removing the ground reported the flower as kept: changes = %v", changes)
	}
	if got := w.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("grass without support = %d, want %d (air)", got, airState)
	}
	if got := w.BlockAt(2, GroundLevel, 0); got != airState {
		t.Errorf("flower without support = %d, want %d (air)", got, airState)
	}
}

// findChange returns the state a position was left in by a change set, or
// fails a test wanting to know.
func findChange(t *testing.T, changes []BlockChange, x, y, z int32) uint16 {
	t.Helper()
	for _, change := range changes {
		if change.X == x && change.Y == y && change.Z == z {
			return change.State
		}
	}
	t.Fatalf("no change recorded for (%d, %d, %d) in %v", x, y, z, changes)
	return 0
}

// The fluid tick's own writes: a flow that dries to air and a flow that takes
// a feeder's level are writes inside the visit, not a setBlock the server
// drives, and each runs the shape pass of a flags-3 setBlock so an observer
// watching the cell hears the level move.
// vanilla: FlowingFluid.tick, Level.setBlock
