package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// A disk written under a tall plant takes both halves with it once the chunk
// finishes: the lower half cannot stand on sand, and the upper half stands on
// the lower. Until then both stay.
// vanilla: Feature.markAboveForPostProcessing
func TestDiskClearsTallPlantAboveIt(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	r.nx, r.nz = 0, 0
	state := func(name string, props map[string]string) uint16 {
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	grass, sand := world.states.grass, world.states.sand
	lower := state("minecraft:tall_grass", map[string]string{"half": "lower"})
	upper := state("minecraft:tall_grass", map[string]string{"half": "upper"})
	const y = 80
	r.set(8, y, 8, grass)
	r.set(8, y+1, 8, lower)
	r.set(8, y+2, 8, upper)

	place := compileFeature(parseJSON(`{"type":"disk","config":{
		"half_height":0,"radius":2,
		"target":{"type":"matching_blocks","blocks":"minecraft:grass_block"},
		"state_provider":{"type":"simple_state_provider","state":{"Name":"minecraft:sand"}}
	}}`))
	if !place(r, [3]int32{8, y, 8}) {
		t.Fatal("disk placed nothing")
	}
	for i, want := range []uint16{sand, lower, upper} {
		if got := r.get(8, y+int32(i), 8); got != want {
			t.Errorf("before the chunk finishes, block at y+%d is %d, want %d", i, got, want)
		}
	}
	r.reconnectShapes(r.protoAt(8, 8))
	for i, want := range []uint16{sand, airState, airState} {
		if got := r.get(8, y+int32(i), 8); got != want {
			t.Errorf("block at y+%d is %d, want %d", i, got, want)
		}
	}
}

func TestDiskKeepsUnsupportedRail(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	r.nx, r.nz = 0, 0
	rail, ok := registry.FindState("minecraft:rail", map[string]string{"shape": "north_south"})
	if !ok {
		t.Fatal("no rail state")
	}
	grass, ok := registry.FindState("minecraft:tall_grass", map[string]string{"half": "lower"})
	if !ok {
		t.Fatal("no tall_grass state")
	}
	const y = 80
	r.set(8, y, 8, rail)
	r.set(9, y, 8, grass)
	r.dropUnsupported(8, y, 8)
	r.dropUnsupported(9, y, 8)
	if got := r.get(8, y, 8); got != rail {
		t.Errorf("rail became %d, want %d", got, rail)
	}
	if got := r.get(9, y, 8); got != airState {
		t.Errorf("tall grass is %d, want air", got)
	}
}

func TestDiskDefersUnsupportedCaneUntilTick(t *testing.T) {
	r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
	cane := registry.BlockSugarCane.DefaultState()
	r.set(8, 80, 8, registry.BlockGrassBlock.DefaultState())
	r.set(8, 81, 8, cane)
	r.set(8, 82, 8, cane)
	r.set(8, 80, 8, registry.BlockGravel.DefaultState())
	r.markAbove(8, 80, 8)
	own := r.protoAt(8, 8)
	r.reconnectShapes(own)
	for y := int32(81); y <= 82; y++ {
		if got := r.get(8, y, 8); got != cane {
			t.Errorf("y=%d: got %d, want cane", y, got)
		}
	}
	if len(own.sources) != 1 || own.sources[0] != (posKey{8, 81 - worldMinY, 8}) {
		t.Fatalf("scheduled ticks = %v, want only bottom cane", own.sources)
	}
}

func TestGeneratedCaneSurvivalTick(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprintf("restore=%v", restore), func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			const x, y, z = 8, GroundLevel + 4, 8
			cane := registry.BlockSugarCane.DefaultState()
			w.SetBlock(x, y-1, z, registry.BlockGravel.DefaultState())
			w.SetBlock(x, y, z, cane)
			w.SetBlock(x, y+1, z, cane)
			w.pendingGenerated = append(w.pendingGenerated, posKey{x, y, z})
			if got := w.BlockAt(x, y, z); got != cane {
				t.Fatalf("before first tick: %d, want cane", got)
			}
			if restore {
				w.SetBlock(x, y-1, z, registry.BlockSand.DefaultState())
				w.SetBlock(x+1, y-1, z, waterState)
			}
			p.Tick(1)
			want := uint16(airState)
			if restore {
				want = cane
			}
			if got := w.BlockAt(x, y, z); got != want {
				t.Fatalf("restore=%v: got %d, want %d", restore, got, want)
			}
		})
	}
}

func TestGeneratedMagmaGrowsBubbleColumn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base       registry.Block
		knownShape bool
		want       uint16
	}{
		{"magma", registry.BlockMagmaBlock, false, bubbleDownState},
		{"soul_sand", registry.BlockSoulSand, false, bubbleUpState},
		{"bulk_magma", registry.BlockMagmaBlock, true, waterState},
		{"bulk_soul_sand", registry.BlockSoulSand, true, waterState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
			const y = 80
			for yy := int32(y + 1); yy <= y+3; yy++ {
				r.set(8, yy, 8, waterState)
			}
			if tc.knownShape {
				r.setKnownShape(8, y, 8, tc.base.DefaultState())
			} else {
				r.set(8, y, 8, tc.base.DefaultState())
			}
			for yy := int32(y + 1); yy <= y+3; yy++ {
				if got := r.get(8, yy, 8); got != waterState {
					t.Fatalf("before FULL at y=%d: got %d, want water", yy, got)
				}
			}
			own := r.protoAt(8, 8)
			r.reconnectShapes(own)
			for yy := int32(y + 1); yy <= y+3; yy++ {
				if got := r.get(8, yy, 8); got != tc.want {
					t.Errorf("at y=%d: got %d, want %d", yy, got, tc.want)
				}
			}
			if len(own.sources) != 0 {
				t.Errorf("unexpected fluid visits: %v", own.sources)
			}
		})
	}
}

// A bubble column grown beside a tall seagrass over magma breaks it into water
// when the column comes first; when the seagrass's own mark comes first, the
// lower half goes to air without updating the upper half, which the column
// then breaks.
// vanilla: BubbleColumnBlock.updateColumn, LevelChunk.postProcessGeneration
func TestGeneratedBubbleColumnBreaksSeagrassBeside(t *testing.T) {
	lower, _ := registry.WithProp(registry.BlockTallSeagrass.DefaultState(), registry.PropHalf, "lower")
	upper, _ := registry.WithProp(registry.BlockTallSeagrass.DefaultState(), registry.PropHalf, "upper")
	const y = 80
	for _, tc := range []struct {
		name         string
		columnFirst  bool
		wantY1, want uint16
	}{
		{"column first", true, bubbleDownState, bubbleDownState},
		{"seagrass first", false, airState, waterState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDecorRegion(newGoldenTerrain(), newChunk(0, 0), worldMinY, worldHeight)
			for yy := int32(y + 1); yy <= y+3; yy++ {
				r.set(8, yy, 8, waterState)
				r.set(8, yy, 9, waterState)
			}
			r.set(8, y+1, 9, lower)
			r.set(8, y+2, 9, upper)
			order := [][2]int32{{8, 8}, {8, 9}}
			if !tc.columnFirst {
				order[0], order[1] = order[1], order[0]
			}
			for _, p := range order {
				r.set(p[0], y, p[1], registry.BlockMagmaBlock.DefaultState())
			}
			r.reconnectShapes(r.protoAt(8, 8))
			if got := r.get(8, y+1, 9); got != tc.wantY1 {
				t.Errorf("lower half: got %d, want %d", got, tc.wantY1)
			}
			if got := r.get(8, y+2, 9); got != tc.want {
				t.Errorf("upper half: got %d, want %d", got, tc.want)
			}
			for yy := int32(y + 1); yy <= y+3; yy++ {
				if got := r.get(8, yy, 8); got != bubbleDownState {
					t.Errorf("column at y=%d: got %d, want drag column", yy, got)
				}
			}
		})
	}
}
