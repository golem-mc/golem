package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// growOneOak grows an oak on a stage-1 sapling at (5, 64, 0), its trunk
// roll pinned, the way growCrop's advanceTree reaches it.
func growOneOak(t *testing.T, w *World, p *Physics, seed int64) {
	t.Helper()
	p.rng = rand.New(rand.NewSource(seed))
	sapling, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no oak sapling")
	}
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the registry holds no grass block")
	}
	w.SetBlock(5, 63, 0, grass)
	w.SetBlock(5, 64, 0, sapling)
	var changes []BlockChange
	p.placeGrownFeature("oak", true, 5, 64, 0, []BlockChange{{X: 5, Y: 64, Z: 0, State: airState}}, &changes)
}

// TestFancyOakWithNoRoomDoesNotGrow checks the fancy oak's free-height
// check: a block on its own trunk column within the minimum clipped height
// of four refuses the tree whole, the sapling left standing.
func TestFancyOakWithNoRoomDoesNotGrow(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(42))
	sapling, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no stage-1 oak sapling")
	}
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the registry holds no grass block")
	}
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry holds no stone")
	}
	w.SetBlock(5, 63, 0, grass)
	w.SetBlock(5, 64, 0, sapling)
	w.SetBlock(5, 65, 0, stone)
	var changes []BlockChange
	p.placeGrownFeature("fancy_oak", true, 5, 64, 0, []BlockChange{{X: 5, Y: 64, Z: 0, State: airState}}, &changes)
	if name, props, _ := registry.BlockStateName(w.BlockAt(5, 64, 0)); name != "minecraft:oak_sapling" || registry.ParseStateProps(props)["stage"] != "1" {
		t.Errorf("the sapling became %s %v, want the stage-1 sapling it was", name, props)
	}
	for dx := int32(-6); dx <= 6; dx++ {
		for dz := int32(-6); dz <= 6; dz++ {
			for y := int32(64); y <= 80; y++ {
				if name, _, _ := registry.BlockStateName(w.BlockAt(5+dx, y, dz)); name == "minecraft:oak_log" {
					t.Fatalf("a log stands at (%d, %d, %d) though the stone denies the tree", 5+dx, y, dz)
				}
			}
		}
	}
}

// TestFancyOakClipsUnderACeiling checks the fancy oak's clipping: a block on
// the trunk column inside the rolled height clips the tree to the free
// height, which still reaches the minimum of four, so a shorter tree grows
// rather than none (vanilla TreeFeature.getMaxFreeTreeHeight).
func TestFancyOakClipsUnderACeiling(t *testing.T) {
	w, p := newPhysicsWorld(t)
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the registry holds no grass block")
	}
	sapling, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no stage-1 oak sapling")
	}
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry holds no stone")
	}
	w.SetBlock(5, 63, 0, grass)
	w.SetBlock(5, 64, 0, sapling)
	w.SetBlock(5, 72, 0, stone) // eight above: free height six

	var changes []BlockChange
	p.rng = rand.New(rand.NewSource(17)) // rolls 13, clipped to the free six
	p.placeGrownFeature("fancy_oak", true, 5, 64, 0, []BlockChange{{X: 5, Y: 64, Z: 0, State: airState}}, &changes)

	trunk := int32(0)
	for {
		if name, _, _ := registry.BlockStateName(w.BlockAt(5, 64+trunk, 0)); name != "minecraft:oak_log" {
			break
		}
		trunk++
	}
	if trunk < 4 || trunk > 5 {
		t.Fatalf("the clipped fancy oak stands %d logs tall, want 4 to 5", trunk)
	}
	if name, _, _ := registry.BlockStateName(w.BlockAt(5, 72, 0)); name != "minecraft:stone" {
		t.Errorf("the ceiling became %s, want the stone it was", name)
	}
}

// TestFancyOakNearTheBuildLimitRefusesATallRoll checks the world-top guard:
// a rolled height that would pass the world's top refuses the tree whole,
// rather than growing it clipped (vanilla TreeFeature.doPlace).
func TestFancyOakNearTheBuildLimitRefusesATallRoll(t *testing.T) {
	w, p := newPhysicsWorld(t)
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the registry holds no grass block")
	}
	sapling, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no stage-1 oak sapling")
	}
	w.SetBlock(5, 309, 0, grass)
	w.SetBlock(5, 310, 0, sapling)

	var changes []BlockChange
	p.rng = rand.New(rand.NewSource(17)) // rolls 13, past the ten that can reach the top
	p.placeGrownFeature("fancy_oak", true, 5, 310, 0, []BlockChange{{X: 5, Y: 310, Z: 0, State: airState}}, &changes)

	for y := int32(310); y <= 330; y++ {
		if name, _, _ := registry.BlockStateName(w.BlockAt(5, y, 0)); name == "minecraft:oak_log" {
			t.Fatalf("a log stands at (5, %d, 0) though the rolled height passes the world top", y)
		}
	}
	if name, props, _ := registry.BlockStateName(w.BlockAt(5, 310, 0)); name != "minecraft:oak_sapling" || registry.ParseStateProps(props)["stage"] != "1" {
		t.Errorf("the sapling stands as %s %v, want the stage-1 sapling it was", name, props)
	}
}

// TestGrowTreeLeavesOutsideTheBoxKeepTheirDistance checks the distance
// walk's bound: a neighbouring tree's leaves outside the grown tree's own
// box are not rewritten (vanilla bounds.isInside).
func TestGrowTreeLeavesOutsideTheBoxKeepTheirDistance(t *testing.T) {
	w, p := newPhysicsWorld(t)
	// The untouched leaves stand at seven, the distance the walk would
	// lower if it reached them.
	leafD7, ok := registry.FindState("minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "false"})
	if !ok {
		t.Fatal("the registry holds no distance-7 oak leaves")
	}
	// A block of leaves just past the canopy's east edge (x=8 is dx=3),
	// at every height an oak crown can take: the tree's own edge at dx=2
	// touches it, so the walk can reach it only by leaving the box.
	for y := int32(65); y <= 70; y++ {
		for x := int32(8); x <= 10; x++ {
			w.SetBlock(x, y, 0, leafD7)
		}
	}

	growOneOak(t, w, p, 1)

	// x=8 and out lie outside the tree's box and must stand as they were.
	for y := int32(65); y <= 70; y++ {
		for x := int32(8); x <= 10; x++ {
			if got := w.BlockAt(x, y, 0); got != leafD7 {
				name, props, _ := registry.BlockStateName(got)
				t.Errorf("the neighbouring leaf at (%d, %d, %d) became %s %v, want it untouched", x, y, 0, name, props)
			}
		}
	}
}

// saplingTickSeed is a seed whose first nextInt(7) is zero, so a sapling
// random tick in light of nine or more reaches advanceTree; its next draw,
// a Float32 above the oak's 0.1 secondary chance, picks the plain tree.
const saplingTickSeed = 18

// oakSapling resolves an oak sapling at the given stage, failing the test
// when the registry does not hold it.
func oakSapling(t *testing.T, stage string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": stage})
	if !ok {
		t.Fatalf("the registry holds no stage-%s oak sapling", stage)
	}
	return state
}

// litOakSapling lays a stage-N oak sapling on grass at (5, 64, 0), asserts
// the cell above it stands in raw light of nine or more, and returns the
// state's props for the tick to read.
func litOakSapling(t *testing.T, w *World, p *Physics, stage string) string {
	t.Helper()
	setBlock(t, w, p, 5, 63, 0, registry.BlockStates["minecraft:grass_block"])
	setBlock(t, w, p, 5, 64, 0, oakSapling(t, stage))
	if light := w.MaxLocalRawLightAt(5, 65, 0); light < 9 {
		t.Fatalf("the sapling's above cell stands at light %d, want nine or more", light)
	}
	return statePropsOf(oakSapling(t, stage))
}

// darkAbove roofs a 13x13 at (x, y+2, z) and relights, leaving the cell at
// (x, y+1, z) at light eight — the nearest open sky seven columns away.
func darkAbove(t *testing.T, w *World, x, y, z int32) {
	t.Helper()
	for dx := int32(-6); dx <= 6; dx++ {
		for dz := int32(-6); dz <= 6; dz++ {
			w.SetBlock(x+dx, y+2, z+dz, registry.BlockStates["minecraft:stone"])
		}
	}
	w.Relight(x, z)
}

// mangrovePropaguleState resolves a mangrove propagule state, failing the
// test when the registry does not hold it.
func mangrovePropaguleState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:mangrove_propagule", overrides)
	if !ok {
		t.Fatalf("the registry holds no mangrove propagule %v", overrides)
	}
	return state
}

// TestSaplingRandomTickCyclesTheStage checks the first half of the vanilla
// SaplingBlock.randomTick: a stage-0 sapling in raw light of nine or more,
// rolled a zero, cycles to stage 1 — the invisible write, so no block change
// is handed back.
func TestSaplingRandomTickCyclesTheStage(t *testing.T) {
	w, p := newPhysicsWorld(t)
	litOakSapling(t, w, p, "0")

	p.rng = rand.New(rand.NewSource(saplingTickSeed))
	var changes []BlockChange
	p.saplingTick(5, 64, 0, w.BlockAt(5, 64, 0), &changes)

	if name, props, _ := registry.BlockStateName(w.BlockAt(5, 64, 0)); name != "minecraft:oak_sapling" || registry.ParseStateProps(props)["stage"] != "1" {
		t.Errorf("the sapling stands as %s %s, want stage=1", name, props)
	}
	if len(changes) != 0 {
		t.Errorf("the stage cycle handed back %d block changes, want none (the flags-260 write)", len(changes))
	}
}

// TestSaplingRandomTickGrowsTheTree checks the other half: a stage-1 sapling
// in the same light, rolled a zero, grows its tree through advanceTree, the
// trunk standing where the sapling stood.
func TestSaplingRandomTickGrowsTheTree(t *testing.T) {
	w, p := newPhysicsWorld(t)
	litOakSapling(t, w, p, "1")

	p.rng = rand.New(rand.NewSource(saplingTickSeed))
	var changes []BlockChange
	p.saplingTick(5, 64, 0, w.BlockAt(5, 64, 0), &changes)

	logs := 0
	for y := int32(64); y <= 75; y++ {
		if name, _, _ := registry.BlockStateName(w.BlockAt(5, y, 0)); name == "minecraft:oak_log" {
			logs++
		}
	}
	if logs < 4 {
		t.Fatalf("the grown oak stands %d logs tall, want a trunk of at least four", logs)
	}
}

// TestSameSpecies2x2SaplingsGrow checks mega selection with mixed stages;
// oak still selects a single tree because its grower has no mega feature.
func TestSameSpecies2x2SaplingsGrow(t *testing.T) {
	const seed = 1
	for _, c := range []struct {
		name  string
		log   string
		mega  bool
		mixed bool
	}{
		{name: "minecraft:spruce_sapling", log: "minecraft:spruce_log", mega: true},
		{name: "minecraft:spruce_sapling", log: "minecraft:spruce_log", mega: true, mixed: true},
		{name: "minecraft:jungle_sapling", log: "minecraft:jungle_log", mega: true},
		{name: "minecraft:oak_sapling", log: "minecraft:oak_log"},
	} {
		label := c.name
		if c.mixed {
			label += " mixed stages"
		}
		t.Run(label, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			stage1, ok := registry.FindState(c.name, map[string]string{"stage": "1"})
			if !ok {
				t.Fatalf("the registry holds no stage-1 %s", c.name)
			}
			stage0, ok := registry.FindState(c.name, map[string]string{"stage": "0"})
			if !ok {
				t.Fatalf("the registry holds no stage-0 %s", c.name)
			}
			grass := registry.BlockStates["minecraft:grass_block"]
			// placed keeps each cell's sapling so a mixed 2x2's stage-0
			// neighbours can be checked still standing.
			var placed [2][2]uint16
			for x := int32(5); x <= 6; x++ {
				for z := int32(0); z <= 1; z++ {
					state := stage1
					if c.mixed && (x != 5 || z != 0) {
						state = stage0
					}
					setBlock(t, w, p, x, 63, z, grass)
					setBlock(t, w, p, x, 64, z, state)
					placed[x-5][z] = state
				}
			}
			p.rng = rand.New(rand.NewSource(seed))
			var changes []BlockChange
			p.advanceTree(5, 64, 0, placed[0][0], &changes)

			if !c.mega {
				if name, _, _ := registry.BlockStateName(w.BlockAt(5, 64, 0)); name != c.log {
					t.Errorf("single oak tree = %s, want %s", name, c.log)
				}
				return
			}
			for dx := range int32(2) {
				for dz := range int32(2) {
					if name, _, _ := registry.BlockStateName(w.BlockAt(5+dx, 64, dz)); name != c.log {
						t.Errorf("mega trunk (%d,%d) = %s, want %s", dx, dz, name, c.log)
					}
				}
			}
		})
	}
}

// TestSpruceCrownDoesNotReachPastItsPlacedCells checks the spruce's distance
// walk bound: a crown whose rolled radius is three but whose rows never
// reach three leaves a neighbour three blocks out alone (vanilla
// TreeFeature.updateLeaves bounds by the placed cells, not the rolled
// radius).
func TestSpruceCrownDoesNotReachPastItsPlacedCells(t *testing.T) {
	w, p := newPhysicsWorld(t)
	sapling, ok := registry.FindState("minecraft:spruce_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no stage-1 spruce sapling")
	}
	neighbour, ok := registry.FindState("minecraft:spruce_leaves", map[string]string{"distance": "7", "persistent": "false"})
	if !ok {
		t.Fatal("the registry holds no distance-7 spruce leaves")
	}
	setBlock(t, w, p, 5, 63, 0, registry.BlockStates["minecraft:grass_block"])
	setBlock(t, w, p, 5, 64, 0, sapling)
	// The neighbour stands three out (dx=3) at every crown height: the
	// rolled radius reaches it, the placed cone does not.
	for y := int32(64); y <= 76; y++ {
		setBlock(t, w, p, 8, y, 0, neighbour)
	}

	p.rng = rand.New(rand.NewSource(1)) // rolls radius three, a five-row crown
	var changes []BlockChange
	p.advanceTree(5, 64, 0, sapling, &changes)

	for y := int32(64); y <= 76; y++ {
		if got := w.BlockAt(8, y, 0); got != neighbour {
			name, props, _ := registry.BlockStateName(got)
			t.Errorf("the neighbouring leaf at (8, %d, 0) became %s %s, want it untouched", y, name, props)
		}
	}
}

// TestSpruceTrunkOnlyBottomRow checks the free-height box's limit: the
// spruce's first level is its trunk column alone (vanilla
// TwoLayersFeatureSize(2, 0, 2)), so a block one beside it, one above the
// sapling, does not refuse the tree, while the acacia's 5x5 from the first
// level (TwoLayersFeatureSize(1, 0, 2)) does turn the same block away.
func TestSpruceTrunkOnlyBottomRow(t *testing.T) {
	t.Run("spruce", func(t *testing.T) {
		w, p := newPhysicsWorld(t)
		sapling, ok := registry.FindState("minecraft:spruce_sapling", map[string]string{"stage": "1"})
		if !ok {
			t.Fatal("the registry holds no stage-1 spruce sapling")
		}
		setBlock(t, w, p, 5, 63, 0, registry.BlockStates["minecraft:grass_block"])
		setBlock(t, w, p, 5, 64, 0, sapling)
		setBlock(t, w, p, 6, 65, 0, registry.BlockStates["minecraft:stone"])

		p.rng = rand.New(rand.NewSource(1))
		var changes []BlockChange
		p.advanceTree(5, 64, 0, sapling, &changes)

		if name, _, _ := registry.BlockStateName(w.BlockAt(5, 64, 0)); name != "minecraft:spruce_log" {
			t.Errorf("the spruce beside the block grew %s, want the tree its trunk-only first row allows", name)
		}
	})
	t.Run("acacia", func(t *testing.T) {
		w, p := newPhysicsWorld(t)
		sapling, ok := registry.FindState("minecraft:acacia_sapling", map[string]string{"stage": "1"})
		if !ok {
			t.Fatal("the registry holds no stage-1 acacia sapling")
		}
		setBlock(t, w, p, 5, 63, 0, registry.BlockStates["minecraft:grass_block"])
		setBlock(t, w, p, 5, 64, 0, sapling)
		setBlock(t, w, p, 6, 65, 0, registry.BlockStates["minecraft:stone"])

		p.rng = rand.New(rand.NewSource(1))
		var changes []BlockChange
		p.advanceTree(5, 64, 0, sapling, &changes)

		for x := int32(0); x <= 11; x++ {
			for z := int32(-5); z <= 5; z++ {
				for y := int32(64); y <= 82; y++ {
					if name, _, _ := registry.BlockStateName(w.BlockAt(x, y, z)); name == "minecraft:acacia_log" {
						t.Fatalf("an acacia log stands at (%d, %d, %d) though the 5x5 first row denies the tree", x, y, z)
					}
				}
			}
		}
	})
}

// TestSaplingRandomTickUnderNineDrawsNoRoll checks the gate: a sapling whose
// above cell stands at raw light eight is left standing, and no random is
// drawn at all — the brightness is asked before the nextInt(7).
func TestSaplingRandomTickUnderNineDrawsNoRoll(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 8, 63, 8, registry.BlockStates["minecraft:grass_block"])
	setBlock(t, w, p, 8, 64, 8, oakSapling(t, "0"))
	darkAbove(t, w, 8, 64, 8)
	if light := w.MaxLocalRawLightAt(8, 65, 8); light != 8 {
		t.Fatalf("the sapling's above cell stands at light %d, want eight", light)
	}

	p.rng = rand.New(rand.NewSource(saplingTickSeed))
	ref := rand.New(rand.NewSource(saplingTickSeed))
	var changes []BlockChange
	p.saplingTick(8, 64, 8, oakSapling(t, "0"), &changes)

	if name, props, _ := registry.BlockStateName(w.BlockAt(8, 64, 8)); name != "minecraft:oak_sapling" || registry.ParseStateProps(props)["stage"] != "0" {
		t.Errorf("the sapling stands as %s %s, want the stage-0 sapling it was", name, props)
	}
	if len(changes) != 0 {
		t.Errorf("the dark sapling handed back %d block changes, want none", len(changes))
	}
	if got, want := p.rng.Int63(), ref.Int63(); got != want {
		t.Errorf("the dark sapling drew a random (%d, want the source's %d)", got, want)
	}
}

// TestMangrovePropaguleRandomTickAgesInTheDark checks the hanging branch of
// the vanilla MangrovePropaguleBlock.randomTick: an age climbs with no roll
// and no light check, so a propagule whose above cell is under nine still
// ages, and the flags-2 write is handed to the clients.
func TestMangrovePropaguleRandomTickAgesInTheDark(t *testing.T) {
	w, p := newPhysicsWorld(t)
	hanging := mangrovePropaguleState(t, map[string]string{"hanging": "true", "age": "0"})
	setBlock(t, w, p, 8, 64, 8, hanging)
	darkAbove(t, w, 8, 64, 8)
	if light := w.MaxLocalRawLightAt(8, 65, 8); light >= 9 {
		t.Fatalf("the propagule's above cell stands at light %d, want below nine", light)
	}

	var changes []BlockChange
	p.mangrovePropaguleTick(8, 64, 8, hanging, &changes)

	if name, props, _ := registry.BlockStateName(w.BlockAt(8, 64, 8)); name != "minecraft:mangrove_propagule" || registry.ParseStateProps(props)["age"] != "1" {
		t.Errorf("the propagule stands as %s %s, want age=1", name, props)
	}
	if len(changes) != 1 {
		t.Errorf("the aging handed back %d block changes, want one (the flags-2 write)", len(changes))
	}
}

// TestMangrovePropaguleRandomTickStandingAdvancesStage checks the standing
// branch: the propagule is a sapling, so the one-in-seven roll carries stage
// 0 to stage 1 through advanceTree, again with no light check.
func TestMangrovePropaguleRandomTickStandingAdvancesStage(t *testing.T) {
	w, p := newPhysicsWorld(t)
	standing := mangrovePropaguleState(t, map[string]string{"hanging": "false", "stage": "0"})
	setBlock(t, w, p, 5, 63, 0, registry.BlockStates["minecraft:grass_block"])
	setBlock(t, w, p, 5, 64, 0, standing)

	p.rng = rand.New(rand.NewSource(saplingTickSeed))
	var changes []BlockChange
	p.mangrovePropaguleTick(5, 64, 0, standing, &changes)

	if name, props, _ := registry.BlockStateName(w.BlockAt(5, 64, 0)); name != "minecraft:mangrove_propagule" || registry.ParseStateProps(props)["stage"] != "1" {
		t.Errorf("the propagule stands as %s %s, want stage=1", name, props)
	}
	if len(changes) != 0 {
		t.Errorf("the stage cycle handed back %d block changes, want none (the flags-260 write)", len(changes))
	}
}

// observerPulseTicks drains a booked observer visit over eight engine ticks
// and reports for how many of them the observer at (14, 63, 9) stood
// powered.
func observerPulseTicks(t *testing.T, w *World, p *Physics, clock *int64) int {
	t.Helper()
	pulse := 0
	for range 8 {
		*clock++
		p.Tick(*clock)
		if observerProps(t, w, 14, 63, 9)["powered"] == "true" {
			pulse++
		}
	}
	return pulse
}

// TestASaplingStageBumpPulsesTheObserverWatchingIt checks the flag-260 stage
// write of SaplingBlock.advanceTree: a stage-0 sapling whose stage the bone
// meal advances pulses the observer watching its cell for the two game ticks
// vanilla's pulse lasts, no client ever told of the stage itself.
// vanilla: SaplingBlock.advanceTree
func TestASaplingStageBumpPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	sapling := oakSapling(t, "0")
	setBlock(t, w, p, 13, 63, 9, sapling)
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the stage moved: %v", props)
	}

	var changes []BlockChange
	p.advanceTree(13, 63, 9, sapling, &changes)

	if _, props := rsNameAt(t, w, 13, 63, 9); props["stage"] != "1" {
		t.Fatalf("the sapling stands at stage %q, want 1", props["stage"])
	}
	if len(changes) != 0 {
		t.Errorf("the stage bump handed back %d block changes, want none (the flags-260 write)", len(changes))
	}
	if pulse := observerPulseTicks(t, w, p, clock); pulse != 2 {
		t.Errorf("the stage bump pulsed the observer for %d ticks, want the two-tick pulse", pulse)
	}
}

// TestAHangingPropaguleAgeBumpPulsesTheObserverWatchingIt checks the flag-2
// age write of MangrovePropaguleBlock.randomTick: a hanging propagule whose
// age the random pass climbs pulses the observer watching its cell for the
// two game ticks vanilla's pulse lasts.
// vanilla: MangrovePropaguleBlock.randomTick
func TestAHangingPropaguleAgeBumpPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	hanging := mangrovePropaguleState(t, map[string]string{"hanging": "true", "age": "0"})
	setBlock(t, w, p, 13, 63, 9, hanging)
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the age moved: %v", props)
	}

	var changes []BlockChange
	p.mangrovePropaguleTick(13, 63, 9, hanging, &changes)

	if _, props := rsNameAt(t, w, 13, 63, 9); props["age"] != "1" {
		t.Fatalf("the propagule stands at age %q, want 1", props["age"])
	}
	if pulse := observerPulseTicks(t, w, p, clock); pulse != 2 {
		t.Errorf("the age bump pulsed the observer for %d ticks, want the two-tick pulse", pulse)
	}
}

// TestGrownBeeNestTreeKeepsItsBees checks that a grown tree's bee nest holds
// the bees its decorator put in; fancy_oak_bees places a nest every time.
// vanilla: BeehiveDecorator.place
func TestGrownBeeNestTreeKeepsItsBees(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(1))
	sapling, ok := registry.FindState("minecraft:oak_sapling", map[string]string{"stage": "1"})
	if !ok {
		t.Fatal("the registry holds no oak sapling")
	}
	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the registry holds no grass block")
	}
	w.SetBlock(5, 63, 0, grass)
	w.SetBlock(5, 64, 0, sapling)
	var changes []BlockChange
	p.placeGrownFeature("fancy_oak_bees", true, 5, 64, 0, []BlockChange{{X: 5, Y: 64, Z: 0, State: airState}}, &changes)
	nests := 0
	for _, c := range changes {
		if registry.BlockOf(c.State) != registry.BlockBeeNest {
			continue
		}
		nests++
		tag, ok := w.BlockEntityTag(c.X, c.Y, c.Z)
		if !ok {
			t.Fatalf("the bee nest at (%d, %d, %d) has no block entity", c.X, c.Y, c.Z)
		}
		if bees, _ := nbt.Get[nbt.List](tag, "bees"); len(bees.Items) == 0 {
			t.Errorf("the bee nest at (%d, %d, %d) holds no bees: %v", c.X, c.Y, c.Z, tag)
		}
	}
	if nests != 1 {
		t.Fatalf("the grown tree has %d bee nests, want 1", nests)
	}
}
