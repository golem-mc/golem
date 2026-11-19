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

// TestLavaTurnsToObsidianTheMomentWaterAppearsBesideIt checks the wake the
// vanilla LiquidBlock hangs on its own placement and every neighbour change:
// the lava looks for water among the five faces that are not its floor, and
// finding it turns to obsidian over a source and cobblestone over a flow
// before any tick has run, with the hiss of the fizz for the clients.
func TestLavaTurnsToObsidianTheMomentWaterAppearsBesideIt(t *testing.T) {
	w, p := newPhysicsWorld(t)

	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	if !w.SetBlock(1, GroundLevel, 0, lavaByLevel[0]) {
		t.Fatal("setup: SetBlock did not take")
	}
	changes, effects, _ := p.NotifyChanged(1, GroundLevel, 0, airState)

	if findChange(t, changes, 1, GroundLevel, 0) != obsidianState {
		t.Errorf("lava placed beside water = %d in the wake, want %d (obsidian)",
			findChange(t, changes, 1, GroundLevel, 0), obsidianState)
	}
	if got := w.BlockAt(1, GroundLevel, 0); got != obsidianState {
		t.Errorf("lava placed beside water = %d, want %d (obsidian)", got, obsidianState)
	}
	fizzed := false
	for _, effect := range effects {
		if effect.LevelEvent == 1501 && effect.X == 1 && effect.Y == GroundLevel && effect.Z == 0 {
			fizzed = true
		}
	}
	if !fizzed {
		t.Errorf("the wake played %d effects, want the 1501 fizz among them", len(effects))
	}
	// The water itself is untouched: the lava gave way, not the pool.
	assertFluid(t, w, 0, GroundLevel, 0, fluidWater, 0, "the water that won")
}

// TestBasaltFormsWhereSoulSoilMeetsBlueIce checks the one deliberate lava
// placement the wake makes: lava standing on soul soil, with blue ice beside
// it, becomes basalt instead of obsidian.
func TestBasaltFormsWhereSoulSoilMeetsBlueIce(t *testing.T) {
	w, p := newPhysicsWorld(t)

	setBlock(t, w, p, 1, GroundLevel-1, 0, soulSoilState)
	setBlock(t, w, p, 2, GroundLevel, 0, blueIceState)
	if !w.SetBlock(1, GroundLevel, 0, lavaByLevel[0]) {
		t.Fatal("setup: SetBlock did not take")
	}
	changes, _, _ := p.NotifyChanged(1, GroundLevel, 0, airState)

	if findChange(t, changes, 1, GroundLevel, 0) != basaltState {
		t.Errorf("lava over soul soil beside blue ice = %d, want %d (basalt)",
			findChange(t, changes, 1, GroundLevel, 0), basaltState)
	}
}

// TestWaterWaterlogsAStairsBetweenSourcesButNotAFlow checks the two faces of
// the waterlogging rule: a container takes a water source in when the water
// arriving would be a source itself — two sources hemming it in — and holds
// it as a source of its own that leaks out the open sides; but the thin
// water a single source sends across it never gets in at all.
func TestWaterWaterlogsAStairsBetweenSourcesButNotAFlow(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	stairs := registry.BlockStates["minecraft:oak_stairs"]
	wet, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the registry has no waterlogged oak stairs")
	}

	// One source short of a pair: the stairs stay dry, and the water walks
	// around them instead.
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	setBlock(t, w, p, 1, GroundLevel, 0, stairs)
	tickToQuiet(t, p, &clock, 300)
	if got := w.BlockAt(1, GroundLevel, 0); got != stairs {
		t.Errorf("stairs beside one source = %d, want %d (dry stairs)", got, stairs)
	}

	// A pair of sources either side. The wall rows run two past the
	// sources on both sides, far enough that their floods thin to nothing
	// before they can curl around an end and creep back in: the two cells
	// north and south of the stairs are fed by nothing but the water the
	// stairs itself holds.
	for x := int32(-2); x <= 4; x++ {
		if x != 1 { // the stairs' own leak-ways stay open
			setBlock(t, w, p, x, GroundLevel, 7, stoneState)
			setBlock(t, w, p, x, GroundLevel, 9, stoneState)
		}
	}
	setBlock(t, w, p, 0, GroundLevel, 8, waterByLevel[0])
	setBlock(t, w, p, 2, GroundLevel, 8, waterByLevel[0])
	setBlock(t, w, p, 1, GroundLevel, 8, stairs)
	tickToQuiet(t, p, &clock, 500)

	if got := w.BlockAt(1, GroundLevel, 8); got != wet {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("stairs between two sources = %s %q, want waterlogged stairs", name, props)
	}
	// The stairs face north, so its tall half walls its own north face —
	// the water it holds cannot leave that way, vanilla geometry and all.
	assertAir(t, w, 1, GroundLevel, 7, "behind the stairs' tall half")
	assertFluid(t, w, 1, GroundLevel, 9, fluidWater, 1, "the waterlogging leaks")
}

// TestWaterRunsDownATrenchToAHole checks the slope search end to end: a
// trench with a hole at its far end drains toward the hole — the water
// thinning along the path, nothing spilling behind the source, the hole
// taking the pour as a falling column — and the column landing on stone
// finds every side walled and stops.
func TestWaterRunsDownATrenchToAHole(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	// A trench one wide running east, walled north and south, with a
	// three-deep shaft at its far end.
	for x := int32(-1); x <= 6; x++ {
		setBlock(t, w, p, x, GroundLevel, -1, stoneState)
		setBlock(t, w, p, x, GroundLevel, 1, stoneState)
	}
	for y := int32(GroundLevel - 3); y < GroundLevel; y++ {
		setBlock(t, w, p, 5, y, 0, airState)
	}
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 500)

	assertFluid(t, w, 1, GroundLevel, 0, fluidWater, 1, "the path to the hole")
	assertFluid(t, w, 2, GroundLevel, 0, fluidWater, 2, "the path to the hole")
	assertFluid(t, w, 3, GroundLevel, 0, fluidWater, 3, "the path to the hole")
	assertFluid(t, w, 4, GroundLevel, 0, fluidWater, 4, "the path to the hole")
	assertFluid(t, w, 5, GroundLevel, 0, fluidWater, 5, "the lip over the hole")
	assertFluid(t, w, 5, GroundLevel-1, 0, fluidWater, fallingLevel, "the fall")
	assertFluid(t, w, 5, GroundLevel-2, 0, fluidWater, fallingLevel, "the fall")
	assertFluid(t, w, 5, GroundLevel-3, 0, fluidWater, fallingLevel, "the shaft floor")
	// Nothing ran on past the hole, nothing spilled behind the source,
	// and the shaft's stone sides held the landing.
	assertAir(t, w, 6, GroundLevel, 0, "past the hole")
	assertAir(t, w, -1, GroundLevel, 0, "behind the source")
	if got := w.BlockAt(4, GroundLevel-3, 0); got != stoneState {
		t.Errorf("stone beside the shaft floor = %d, want %d", got, stoneState)
	}
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

// TestLavaBurnsAPlantAwayWithOnlyAHiss checks lava's half of the same rule:
// what it takes goes with no loot at all, only the 1501 fizz for the
// clients to play.
func TestLavaBurnsAPlantAwayWithOnlyAHiss(t *testing.T) {
	w, p := newPhysicsWorld(t)
	grass := registry.BlockStates["minecraft:short_grass"]

	for x := int32(-1); x <= 3; x++ {
		setBlock(t, w, p, x, GroundLevel, -1, stoneState)
		setBlock(t, w, p, x, GroundLevel, 1, stoneState)
	}
	setBlock(t, w, p, 3, GroundLevel, 0, stoneState)
	// Short grass needs its soil, or the flow's neighbour update pops it
	// before the lava reaches it.
	setBlock(t, w, p, 1, GroundLevel-1, 0, registry.BlockStates["minecraft:dirt"])
	setBlock(t, w, p, 1, GroundLevel, 0, grass)
	setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])

	var clock int64
	hissed, dropped := false, false
	for clock < 400 {
		clock++
		_, _, _, effects := p.Tick(clock)
		for _, effect := range effects {
			if effect.LevelEvent == 1501 && effect.X == 1 && effect.Y == GroundLevel && effect.Z == 0 {
				hissed = true
			}
			if effect.BreakDrop == grass {
				dropped = true
			}
		}
	}
	if !hissed {
		t.Error("the burn played no fizz for the grass")
	}
	if dropped {
		t.Error("the burn dropped loot for the grass")
	}
	assertFluid(t, w, 1, GroundLevel, 0, fluidLava, 2, "the lava in the grass's place")
}

// TestASlabHoldsAWellOfWater checks the face the other way: a source
// standing on a bottom slab is held by it — the slab is somewhere the flow
// cannot hold up against, so the well keeps its water and the slab stays
// dry, a falling column never being enough to waterlog anything.
func TestASlabHoldsAWellOfWater(t *testing.T) {
	w, p := newPhysicsWorld(t)
	slab, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "bottom"})
	if !ok {
		t.Fatal("the registry has no oak slab")
	}
	var clock int64

	setBlock(t, w, p, 0, GroundLevel-1, 0, slab)
	setBlock(t, w, p, 1, GroundLevel, 0, stoneState)
	setBlock(t, w, p, -1, GroundLevel, 0, stoneState)
	setBlock(t, w, p, 0, GroundLevel, 1, stoneState)
	setBlock(t, w, p, 0, GroundLevel, -1, stoneState)
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	tickToQuiet(t, p, &clock, 300)

	assertFluid(t, w, 0, GroundLevel, 0, fluidWater, 0, "the well's water")
	if got := w.BlockAt(0, GroundLevel-1, 0); got != slab {
		t.Errorf("the slab under the well = %d, want %d (dry slab)", got, slab)
	}
	if got := w.BlockAt(0, GroundLevel-2, 0); got != stoneState {
		t.Errorf("under the slab = %d, want %d (stone)", got, stoneState)
	}
}

// TestAFaceTwoHalvesCoverIsAWall checks the face test's own arithmetic: two
// shapes each touching the face from one side cover it between them and
// block the flow, one alone leaves the rest of the face open; and the
// registry's own states answer the way the vanilla shapes do — a top slab's
// ceiling is a wall a fluid cannot pass, a bottom slab's underside is not.
func TestAFaceTwoHalvesCoverIsAWall(t *testing.T) {
	lower := []box{boxF(8, 0, 0, 16, 8, 16)}
	upper := []box{boxF(0, 8, 0, 8, 16, 16)}
	if !mergedFaceOccludes(lower, upper, 0, true) {
		t.Error("two half faces that between them cover the face read as open")
	}
	if mergedFaceOccludes(lower, []box{boxF(0, 0, 0, 8, 8, 16)}, 0, true) {
		t.Error("one half face read as covering the whole face")
	}

	top, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "top"})
	if !ok {
		t.Fatal("the registry has no top oak slab")
	}
	bottom := registry.BlockStates["minecraft:oak_slab"]
	if passWall(airState, 1, false, top) {
		t.Error("a fluid reads through the full ceiling a top slab turns to it")
	}
	if !passWall(airState, 1, false, bottom) {
		t.Error("a fluid reads a wall where a bottom slab only half fills its cell")
	}
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

// TestSandRestsOnAKelpColumn checks the rest test a water plant's cell does
// not change: a falling block falls through air, the fire tag, a fluid block
// and a replaceable one, and kelp is none of those however much water its
// own cell holds. A sand block left on top of a kelp column stays there
// rather than falling through it.
// vanilla: FallingBlock.isFree
func TestSandRestsOnAKelpColumn(t *testing.T) {
	w, p := newPhysicsWorld(t)
	sand := registry.BlockStates["minecraft:sand"]
	kelp := registry.MustState("minecraft:kelp", nil)
	var clock int64

	// A kelp column on the ground with a sand block resting on its top.
	setBlock(t, w, p, 0, GroundLevel, 0, kelp)
	setBlock(t, w, p, 0, GroundLevel+1, 0, kelp)
	setBlock(t, w, p, 0, GroundLevel+2, 0, sand)

	clock += 3
	_, falls, _, _ := p.Tick(clock)
	if len(falls) != 0 {
		t.Fatalf("the sand fell through the kelp: %d falls", len(falls))
	}
	if got := w.BlockAt(0, GroundLevel+2, 0); got != sand {
		t.Errorf("block on top of the kelp = %d, want %d (sand)", got, sand)
	}
}

// TestWaterloggedFallingKeepsItsWater checks the one block that leaves as a
// falling entity carrying water of its own: a waterlogged scaffold handed
// over by its stability tick leaves the water it held in its cell, and the
// body carries the block with the waterlogging taken off it — at the bottom
// the recompute read over the air below it.
func TestWaterloggedFallingKeepsItsWater(t *testing.T) {
	w, p := newPhysicsWorld(t)
	scaffold, ok := registry.FindState("minecraft:scaffolding", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the registry has no waterlogged scaffolding")
	}
	dry, ok := registry.FindState("minecraft:scaffolding", map[string]string{
		"waterlogged": "false", "bottom": "true",
	})
	if !ok {
		t.Fatal("the registry has no dry scaffolding standing on its bottom")
	}
	water := fluidStateAt(fluidWater, 0)
	var clock int64

	setBlock(t, w, p, 0, GroundLevel+6, 0, scaffold)
	clock += 2
	changes, falls, _, _ := p.Tick(clock)
	if len(falls) != 1 {
		t.Fatalf("the tick handed over %d falls, want 1", len(falls))
	}
	if falls[0].State != dry {
		t.Errorf("the body carries state %d, want %d (dry scaffolding)", falls[0].State, dry)
	}
	if got := w.BlockAt(0, GroundLevel+6, 0); got != water {
		t.Errorf("cell the scaffold left = %d, want %d (water)", got, water)
	}
	if findChange(t, changes, 0, GroundLevel+6, 0) != water {
		t.Error("the fall did not report the water the cell kept")
	}
}

// vanilla: SpeleothemBlock.spawnFallingStalactite, FallingBlockEntity.fall
func TestWaterloggedStalactiteKeepsItsShapeAndWater(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, top, z = int32(8), GroundLevel + 8, int32(8)
	thicknesses := []string{"base", "middle", "frustum", "tip"}
	dry := make([]uint16, len(thicknesses))
	setBlock(t, w, p, x, top+1, z, registry.BlockStates["minecraft:stone"])
	for i, thickness := range thicknesses {
		props := map[string]string{
			"vertical_direction": "down", "thickness": thickness, "waterlogged": "false",
		}
		dry[i] = registry.MustState("minecraft:pointed_dripstone", props)
		props["waterlogged"] = "true"
		setBlock(t, w, p, x, top-int32(i), z, registry.MustState("minecraft:pointed_dripstone", props))
	}

	setBlock(t, w, p, x, top+1, z, airState)
	p.Tick(1)
	changes, falls, _, _ := p.Tick(2)
	if len(falls) != len(thicknesses) {
		t.Fatalf("got %d falls, want %d", len(falls), len(thicknesses))
	}
	water := fluidStateAt(fluidWater, 0)
	for i, fall := range falls {
		y := top - int32(i)
		if fall.X != x || fall.Y != y || fall.Z != z {
			t.Errorf("%s spawn at (%d, %d, %d), want (%d, %d, %d)", thicknesses[i], fall.X, fall.Y, fall.Z, x, y, z)
		}
		if fall.State != dry[i] {
			t.Errorf("%s carries %v, want %v", thicknesses[i], registry.StateProps(fall.State), registry.StateProps(dry[i]))
		}
		if got := w.BlockAt(x, y, z); got != water {
			t.Errorf("%s left state %d, want water %d", thicknesses[i], got, water)
		}
		if got := findChange(t, changes, x, y, z); got != water {
			t.Errorf("%s reported state %d, want water %d", thicknesses[i], got, water)
		}
	}
}

// TestAStalactiteStopsAtAMergedTip checks the column walk's end: a stalactite
// whose lowest block is a tip merged into the stalagmite below still stops
// there, the merged tip's body carrying the landing hurt vanilla's
// isTip(state, true) gives it.
// vanilla: SpeleothemBlock.spawnFallingStalactite, isTip
func TestAStalactiteStopsAtAMergedTip(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	stone := registry.BlockStates["minecraft:stone"]
	frustum, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "frustum", "waterlogged": "false",
	})
	if !ok {
		t.Fatal("no pointed dripstone stalactite frustum")
	}
	merged, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "tip_merge", "waterlogged": "false",
	})
	if !ok {
		t.Fatal("no pointed dripstone merged stalactite tip")
	}

	// A support over a frustum over a merged tip.
	setBlock(t, w, p, 0, GroundLevel+2, 0, stone)
	setBlock(t, w, p, 0, GroundLevel+1, 0, frustum)
	setBlock(t, w, p, 0, GroundLevel, 0, merged)

	// Taking the support away books the fall two ticks out.
	setBlock(t, w, p, 0, GroundLevel+2, 0, airState)
	clock++
	p.Tick(clock)
	clock++
	_, falls, _, _ := p.Tick(clock)
	if len(falls) != 2 {
		t.Fatalf("the tick handed over %d falls, want 2 (frustum and merged tip)", len(falls))
	}
	tip := falls[len(falls)-1]
	if tip.State != merged {
		t.Errorf("the lowest body carries state %d, want the merged tip %d", tip.State, merged)
	}
	if !tip.HurtEntities || tip.FallHurtAmount != 6 || tip.FallHurtMax != 40 {
		t.Errorf("the merged tip carries hurt (%v, %g, %d), want (true, 6, 40)",
			tip.HurtEntities, tip.FallHurtAmount, tip.FallHurtMax)
	}
}

// TestAReplacedSupportDoesNotStopAFallingStalactite checks the scheduled tick:
// vanilla's SpeleothemBlock.tick drops a hanging stalactite without testing
// its hold again, so a support put back within the two-tick delay leaves it
// falling.
// vanilla: SpeleothemBlock.tick
func TestAReplacedSupportDoesNotStopAFallingStalactite(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	stone := registry.BlockStates["minecraft:stone"]
	base, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "base", "waterlogged": "false",
	})
	if !ok {
		t.Fatal("no pointed dripstone stalactite base")
	}

	setBlock(t, w, p, 0, GroundLevel+1, 0, stone)
	setBlock(t, w, p, 0, GroundLevel, 0, base)

	// The support goes and comes back inside the delay.
	setBlock(t, w, p, 0, GroundLevel+1, 0, airState)
	setBlock(t, w, p, 0, GroundLevel+1, 0, stone)

	clock++
	p.Tick(clock)
	clock++
	_, falls, _, _ := p.Tick(clock)
	if len(falls) != 1 {
		t.Fatalf("the tick handed over %d falls, want 1 (the stalactite still falls)", len(falls))
	}
	if falls[0].State != base {
		t.Errorf("the body carries state %d, want %d (the stalactite base)", falls[0].State, base)
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

// TestAFloodSettlesWithABoundedQueue floods a hundred sources across a
// chunk border and checks that the engine terminates with a queue
// proportional to the water in the world. One visit per block is what keeps
// it bounded, so a queue that grew past the flooded area's block count would
// mean visits were leaking.
func TestAFloodSettlesWithABoundedQueue(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64

	// Ten sources of ten, straddling a chunk border so the flood has to
	// work across chunks; its reach keeps everything inside what is loaded.
	for x := int32(7); x <= 16; x++ {
		for z := int32(7); z <= 16; z++ {
			setBlock(t, w, p, x, GroundLevel, z, waterByLevel[0])
		}
	}

	// The queue is read by hand rather than through an API because the
	// server has no use for its size; this is the engine's own invariant.
	maxPending := len(p.pending)
	quiet := 0
	for clock < 300 && quiet < 31 {
		clock++
		changes, _, _, _ := p.Tick(clock)
		if len(changes) > 0 {
			quiet = 0
		} else {
			quiet++
		}
		if pending := len(p.pending); pending > maxPending {
			maxPending = pending
		}
	}
	if quiet < 31 {
		t.Fatalf("flood still changing after %d ticks", clock)
	}

	// The flood reached its full extent and no further: five blocks from
	// the nearest source, and seven, and not eight.
	assertFluid(t, w, 2, GroundLevel, 7, fluidWater, 5, "flood's inner field")
	assertFluid(t, w, 23, GroundLevel, 16, fluidWater, 7, "flood's edge")
	assertAir(t, w, 3, GroundLevel, 3, "beyond the reach")

	// The queue at its worst was within the flooded area's block count
	// with room for the ring still spreading: 600-odd blocks of water.
	const bound = 2048
	if maxPending > bound {
		t.Errorf("queue length during the flood = %d, want at most %d", maxPending, bound)
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

// TestASignSupportLossTakesItsBlockEntity checks that a sign popped for
// losing its support leaves nothing behind: the entity holding its text goes
// with the block, so the cell keeps no sign data for whatever stands there
// next.
func TestASignSupportLossTakesItsBlockEntity(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, z = int32(4), int32(7)
	sign, ok := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
	if !ok {
		t.Fatal("the registry has no standing oak sign")
	}
	if !w.SetBlock(x, GroundLevel, z, sign) {
		t.Fatal("setting the sign did not take")
	}
	be := w.EnsureBlockEntity(x, GroundLevel, z, KindSign)
	if be == nil {
		t.Fatal("the sign got no block entity")
	}
	be.Use(func(b *BlockEntity) { b.SignFront.Messages[0] = "old" })

	// Break the support under the sign: the standing sign pops with it.
	setBlock(t, w, p, x, GroundLevel-1, z, airState)
	if got := w.BlockAt(x, GroundLevel, z); got != airState {
		t.Fatalf("block after the support went = %d, want air", got)
	}
	if got := w.BlockEntity(x, GroundLevel, z); got != nil {
		t.Error("the popped sign left its block entity behind")
	}
}

// The fluid tick's own writes: a flow that dries to air and a flow that takes
// a feeder's level are writes inside the visit, not a setBlock the server
// drives, and each runs the shape pass of a flags-3 setBlock so an observer
// watching the cell hears the level move.
// vanilla: FlowingFluid.tick, Level.setBlock

// TestAFlowDryingToAirPulsesTheObserverWatchingIt books one visit of a flow
// with no source left feeding it: the visit dries it to air, and that write
// pulses the observer watching the cell for the two game ticks vanilla's
// pulse lasts.
func TestAFlowDryingToAirPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	// The state goes in without a wake, so the observer stays dark until
	// the visit's own write.
	w.SetBlock(13, 63, 9, waterByLevel[4])
	p.schedule(13, 63, 9, p.now+1)

	*clock++
	p.Tick(*clock)
	if pulse := observerPulseTicks(t, w, p, clock); pulse != 2 {
		t.Errorf("the flow drying to air pulsed the observer for %d ticks, want the two-tick pulse", pulse)
	}
}

// TestAFlowTakingItsFeedersLevelPulsesTheObserverWatchingIt is the level
// change the same visit writes: a thin flow beside a source recomputes to the
// source's level, and that write pulses the observer watching the cell.
func TestAFlowTakingItsFeedersLevelPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	w.SetBlock(12, 63, 9, waterByLevel[0])
	w.SetBlock(13, 63, 9, waterByLevel[7])
	p.schedule(13, 63, 9, p.now+1)

	*clock++
	p.Tick(*clock)
	if got := w.BlockAt(13, 63, 9); got != waterByLevel[1] {
		t.Fatalf("the flow stands at state %d, want %d (level 1)", got, waterByLevel[1])
	}
	if pulse := observerPulseTicks(t, w, p, clock); pulse != 2 {
		t.Errorf("the flow taking its feeder's level pulsed the observer for %d ticks, want the two-tick pulse", pulse)
	}
}
