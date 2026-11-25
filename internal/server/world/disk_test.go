package world

import (
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
		"state_provider":{"type":"minecraft:simple","state":{"id":"minecraft:sand"}}
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
