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
