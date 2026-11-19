package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func mushroomRoof(r *decorRegion, x, y, z, radius int32, state uint16) {
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			r.set(x+dx, y, z+dz, state)
		}
	}
}

func TestOverworldMushroomAutomaticMarks(t *testing.T) {
	t.Parallel()
	for _, b := range []registry.Block{registry.BlockBrownMushroom, registry.BlockRedMushroom} {
		for _, simple := range []bool{false, true} {
			r := overworldMushroomRegion()
			r.set(5, 99, 5, registry.BlockMycelium.DefaultState())
			state := b.DefaultState()
			if simple {
				if !simpleBlock(func(*decorRegion, [3]int32) uint16 { return state }, false)(r, [3]int32{5, 100, 5}) {
					t.Fatal("simple_block failed")
				}
			} else {
				r.set(5, 100, 5, state)
			}
			own := r.protoAt(5, 5)
			if len(own.shapes) != 1 || own.shapes[0] != (posKey{5, 100 - worldMinY, 5}) {
				t.Fatalf("%s simple=%v: marks=%v", b.Name(), simple, own.shapes)
			}
			r.set(5, 99, 5, registry.BlockGrassBlock.DefaultState())
			r.reconnectShapes(own)
			if r.get(5, 100, 5) != airState {
				t.Fatal("FULL retained exposed mushroom")
			}
			n := len(own.shapes)
			r.set(5, worldMinY-1, 5, state)
			r.set(32, 100, 5, state)
			if len(own.shapes) != n {
				t.Fatal("out-of-bounds placement added a mark")
			}
		}
	}
}

func TestOverworldMushroomFullSurvival(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		radius   int32
		roof     registry.Block
		source   registry.Block
		distance int32
		support  registry.Block
		keep     bool
	}{
		{"exposed", -1, registry.BlockAir, registry.BlockAir, 1, registry.BlockStone, false},
		{"covered", 3, registry.BlockStone, registry.BlockAir, 1, registry.BlockGrassBlock, true},
		{"lateral13", 1, registry.BlockStone, registry.BlockAir, 1, registry.BlockStone, false},
		{"lateral12", 2, registry.BlockStone, registry.BlockAir, 1, registry.BlockStone, true},
		{"leaves13", 2, registry.BlockOakLeaves, registry.BlockAir, 1, registry.BlockStone, false},
		{"block13", 3, registry.BlockStone, registry.BlockGlowstone, 2, registry.BlockStone, false},
		{"block12", 3, registry.BlockStone, registry.BlockTorch, 2, registry.BlockStone, true},
		{"tag override", -1, registry.BlockAir, registry.BlockGlowstone, 1, registry.BlockMycelium, true},
		{"lost support", 3, registry.BlockStone, registry.BlockAir, 1, registry.BlockAir, false},
	} {
		for _, b := range []registry.Block{registry.BlockBrownMushroom, registry.BlockRedMushroom} {
			t.Run(tc.name+"/"+b.Name(), func(t *testing.T) {
				r := overworldMushroomRegion()
				r.set(15, 99, 5, tc.support.DefaultState())
				mushroomRoof(r, 15, 101, 5, tc.radius, tc.roof.DefaultState())
				r.set(15+tc.distance, 100, 5, tc.source.DefaultState())
				r.set(15, 100, 5, b.DefaultState())
				r.reconnectShapes(r.protoAt(15, 5))
				if got := r.get(15, 100, 5) == b.DefaultState(); got != tc.keep {
					t.Fatalf("survives=%v, want %v", got, tc.keep)
				}
			})
		}
	}
}

// TestMushroomRecordedInitializationGap replays the seed-0 26.3 recording's
// source-priming window: a neighbour adds cover after the chunk's sources are
// primed and before initialization completes, so the mushroom FEATURES placed
// is lit at FULL and removed; priming at initialization would keep it.
func TestMushroomRecordedInitializationGap(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	g.BeginLightReplay()
	for _, step := range []struct {
		x, z  int32
		phase byte
	}{
		{-114, 16, 'f'},
		{-114, 16, 's'},
		{-113, 16, 'f'},
		{-113, 16, 's'},
		{-114, 17, 'f'},
		{-114, 17, 's'},
		{-113, 17, 'f'},
		{-113, 17, 's'},
		{-114, 16, 'i'},
		{-113, 16, 'i'},
		{-114, 17, 'i'},
		{-113, 17, 'i'},
		{-114, 16, 'l'},
		{-114, 18, 'f'},
		{-114, 18, 's'},
		{-114, 18, 'i'},
		{-113, 18, 'f'},
		{-113, 18, 's'},
		{-113, 18, 'i'},
		{-114, 17, 'l'},
		{-114, 18, 'l'},
		{-112, 16, 'f'},
		{-112, 17, 'f'},
		{-112, 16, 's'},
		{-112, 17, 's'},
		{-112, 18, 'f'},
		{-112, 18, 's'},
		{-112, 16, 'i'},
		{-112, 17, 'i'},
		{-112, 18, 'i'},
		{-113, 16, 'l'},
		{-113, 17, 'l'},
		{-113, 18, 'l'},
		{-112, 16, 'l'},
		{-112, 17, 'l'},
		{-112, 18, 'l'},
	} {
		pos := ChunkPos{X: step.x, Z: step.z}
		switch step.phase {
		case 'f':
			g.DecorateInOrder([]ChunkPos{pos})
		case 's':
			g.PrimeLightSources(pos)
		case 'i', 'l':
			g.ReplayLight(pos, step.phase == 'l')
		}
	}
	// (-1807,63,285)
	own := g.protos.lookup(ChunkPos{-113, 17})
	if state, ok := own.get(1, 63-worldMinY, 13); !ok || registry.BlockOf(state) != registry.BlockBrownMushroom {
		t.Fatal("FEATURES did not place the mushroom at (-1807,63,285)")
	}
	c := newChunk(-113, 17)
	g.Generate(c)
	if got := registry.BlockOf(c.BlockAt(1, 63, 13)); got != registry.BlockAir {
		t.Fatalf("(-1807,63,285): %s, want air", got.Name())
	}
}
