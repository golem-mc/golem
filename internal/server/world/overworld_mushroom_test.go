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

func TestOverworldMushroomSkyEdges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		setup  func(*decorRegion)
		bright bool
	}{
		{"sealed roof", func(r *decorRegion) {
			slab, _ := registry.WithProp(registry.BlockStoneSlab.DefaultState(), registry.PropType, "bottom")
			mushroomRoof(r, 15, 101, 5, 3, slab)
		}, false},
		{"leaves12", func(r *decorRegion) {
			mushroomRoof(r, 15, 102, 5, 3, registry.BlockOakLeaves.DefaultState())
		}, false},
		{"two edge turn across chunk", func(r *decorRegion) {
			mushroomRoof(r, 15, 101, 5, 2, registry.BlockStone.DefaultState())
			r.set(16, 101, 6, airState)
		}, true},
		{"below dimension", func(r *decorRegion) {
			mushroomRoof(r, 15, worldMinY+1, 5, 3, registry.BlockStone.DefaultState())
			r.set(16, worldMinY, 5, registry.BlockStone.DefaultState())
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := overworldMushroomRegion()
			tc.setup(r)
			y := int32(100)
			if tc.name == "below dimension" {
				y = worldMinY
			}
			if got := r.finalLightAtLeast13(15, y, 5); got != tc.bright {
				t.Fatalf("bright=%v, want %v", got, tc.bright)
			}
		})
	}
}

func TestOverworldMushroomOverwrittenMarks(t *testing.T) {
	t.Parallel()
	r := overworldMushroomRegion()
	r.set(5, 100, 5, registry.BlockBrownMushroom.DefaultState())
	r.set(5, 100, 5, registry.BlockTorch.DefaultState())
	own := r.protoAt(5, 5)
	r.reconnectShapes(own)
	if r.get(5, 100, 5) != airState {
		t.Fatal("overwritten torch kept without support")
	}
	r.set(5, 100, 5, registry.BlockStone.DefaultState())
	r.reconnectShapes(own)
	if r.get(5, 100, 5) != registry.BlockStone.DefaultState() {
		t.Fatal("overwritten stone changed")
	}
}

// TestOverworldMushroomRecordedOrder pins a mushroom that survives FULL in the
// seed-0 26.3 recording's FEATURES order, as in overworld_tree_parity_test.go.
func TestOverworldMushroomRecordedOrder(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	g.DecorateInOrder([]ChunkPos{{-115, 17}, {-114, 17}, {-113, 17}, {-115, 18}, {-115, 19}, {-114, 18}, {-114, 19}, {-113, 18}, {-113, 19}})
	c := newChunk(-114, 18)
	g.Generate(c)
	if got := registry.BlockOf(c.BlockAt(12, 64, 11)); got != registry.BlockBrownMushroom {
		t.Fatalf("(-1812,64,299): %s, want brown mushroom", got.Name())
	}
}

func TestOverworldMushroomOverrideNonSolid(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {"minecraft:overrides_mushroom_light_requirement": {Replace: true, Entries: []registry.PackTagEntry{{ID: "minecraft:glass"}}}}})
	r := overworldMushroomRegion()
	r.set(5, 99, 5, registry.BlockGlass.DefaultState())
	r.set(6, 100, 5, registry.BlockGlowstone.DefaultState())
	for _, b := range []registry.Block{registry.BlockBrownMushroom, registry.BlockRedMushroom} {
		r.set(5, 100, 5, b.DefaultState())
		r.reconnectShapes(r.protoAt(5, 5))
		if r.get(5, 100, 5) != b.DefaultState() {
			t.Fatal("tag did not bypass support and light")
		}
	}
}

func TestOverworldMushroomSkySourcesChange(t *testing.T) {
	t.Parallel()
	r := overworldMushroomRegion()
	mushroomRoof(r, 5, 101, 5, 3, registry.BlockStone.DefaultState())
	if r.finalLightAtLeast13(5, 100, 5) {
		t.Fatal("covered position bright")
	}
	r.set(5, 101, 5, airState)
	if !r.finalLightAtLeast13(5, 100, 5) {
		t.Fatal("source floor survived a roof overwrite")
	}
	if !r.finalLightAtLeast13(5, worldMinY+worldHeight-1, 5) {
		t.Fatal("dimension top not a sky source")
	}
}

func TestMushroomKnownShapeWrites(t *testing.T) {
	t.Parallel()
	for _, b := range []registry.Block{registry.BlockBrownMushroom, registry.BlockRedMushroom} {
		r := overworldMushroomRegion()
		r.set(5, 99, 5, registry.BlockGrassBlock.DefaultState())
		tree := &treeBuild{r: r}
		tree.put(&tree.decor, treePos{5, 100, 5}, b.DefaultState())
		own := r.protoAt(5, 5)
		if len(own.shapes) != 0 {
			t.Fatal("known-shape decorator write added a mark")
		}
		r.reconnectShapes(own)
		if r.get(5, 100, 5) != b.DefaultState() {
			t.Fatal("known-shape mushroom changed")
		}
		r.set(5, 100, 5, b.DefaultState())
		r.setKnownShape(5, 100, 5, b.DefaultState())
		r.reconnectShapes(own)
		if r.get(5, 100, 5) != airState {
			t.Fatal("known-shape overwrite erased an earlier mark")
		}
	}
}

func TestMushroomPendingLightSources(t *testing.T) {
	t.Parallel()
	r := overworldMushroomRegion()
	g := r.src.(*OverworldGenerator)
	r.set(5, 9, 5, registry.BlockGrassBlock.DefaultState())
	r.set(5, 10, 5, registry.BlockBrownMushroom.DefaultState())
	r.protoAt(5, 5).decorated = true
	g.BeginLightReplay()
	g.PrimeLightSources(ChunkPos{0, 0})
	mushroomRoof(r, 5, 11, 5, 3, registry.BlockStone.DefaultState())
	g.ReplayLight(ChunkPos{0, 0}, false)
	g.ReplayLight(ChunkPos{0, 0}, true)
	// The neighbour wrote the roof before INITIALIZE_LIGHT completed, so
	// ProtoChunk.setBlockState did not update ChunkSkyLightSources.
	if !r.finalLightAtLeast13(5, 10, 5) {
		t.Fatal("pending initialization lost its primed sky source")
	}
	r.reconnectShapes(r.protoAt(5, 5))
	if r.get(5, 10, 5) != airState {
		t.Fatal("FULL retained a mushroom in published bright sky")
	}
	if got := g.lightReplay[ChunkPos{0, 0}].sources[5*16+5]; got != 10 {
		t.Fatalf("unrelated write repaired source floor: %d", got)
	}
	r.set(5, 12, 5, registry.BlockStone.DefaultState())
	if got := g.lightReplay[ChunkPos{0, 0}].sources[5*16+5]; got != 13 {
		t.Fatalf("new occluder left source floor at %d", got)
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

// TestOverworldMushroomTreeEdgeRecordedOrder replays frozen seed 0 around
// (-199,70,-16), where the tree's edge update must remove its bright mushroom.
func TestOverworldMushroomTreeEdgeRecordedOrder(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(0)
	g.BeginLightReplay()
	for _, s := range []struct {
		x, z  int32
		phase byte
	}{
		{-15, -3, 'f'},
		{-15, -2, 'f'},
		{-15, -1, 'f'},
		{-15, -3, 's'},
		{-15, -2, 's'},
		{-15, -1, 's'},
		{-15, 0, 'f'},
		{-15, 0, 's'},
		{-15, 1, 'f'},
		{-15, 1, 's'},
		{-15, -3, 'i'},
		{-15, -2, 'i'},
		{-15, -1, 'i'},
		{-15, 0, 'i'},
		{-15, 1, 'i'},
		{-14, -1, 'f'},
		{-14, 0, 'f'},
		{-14, 1, 'f'},
		{-14, -1, 's'},
		{-14, 0, 's'},
		{-14, 1, 's'},
		{-14, -3, 'f'},
		{-14, -3, 's'},
		{-14, -2, 'f'},
		{-14, -2, 's'},
		{-13, 0, 'f'},
		{-13, 1, 'f'},
		{-13, 0, 's'},
		{-13, 1, 's'},
		{-13, -3, 'f'},
		{-13, -3, 's'},
		{-13, -2, 'f'},
		{-13, -2, 's'},
		{-13, -1, 'f'},
		{-13, -1, 's'},
		{-14, -1, 'i'},
		{-14, 0, 'i'},
		{-14, 1, 'i'},
		{-14, -3, 'i'},
		{-14, -2, 'i'},
		{-13, 0, 'i'},
		{-13, 1, 'i'},
		{-13, -3, 'i'},
		{-13, -2, 'i'},
		{-13, -1, 'i'},
		{-15, -2, 'l'},
		{-15, -1, 'l'},
		{-15, 0, 'l'},
		{-15, 1, 'l'},
		{-15, -3, 'l'},
		{-14, -1, 'l'},
		{-14, 0, 'l'},
		{-14, 1, 'l'},
		{-14, -3, 'l'},
		{-14, -2, 'l'},
		{-12, 1, 'f'},
		{-12, 1, 's'},
		{-12, 0, 'f'},
		{-12, 0, 's'},
		{-12, -3, 'f'},
		{-12, -3, 's'},
		{-12, -2, 'f'},
		{-12, -2, 's'},
		{-12, -1, 'f'},
		{-12, -1, 's'},
		{-11, -3, 'f'},
		{-11, -2, 'f'},
		{-11, -3, 's'},
		{-11, -2, 's'},
		{-11, -1, 'f'},
		{-11, -1, 's'},
		{-11, 0, 'f'},
		{-11, 0, 's'},
		{-11, 1, 'f'},
		{-11, 1, 's'},
		{-12, 1, 'i'},
		{-12, 0, 'i'},
		{-12, -3, 'i'},
		{-12, -2, 'i'},
		{-12, -1, 'i'},
		{-11, -3, 'i'},
		{-11, -2, 'i'},
		{-13, 0, 'l'},
		{-13, 1, 'l'},
		{-13, -1, 'l'},
		{-13, -3, 'l'},
		{-13, -2, 'l'},
		{-11, -1, 'i'},
		{-11, 0, 'i'},
		{-11, 1, 'i'},
		{-12, -3, 'l'},
		{-12, -2, 'l'},
		{-12, -1, 'l'},
		{-12, 0, 'l'},
		{-12, 1, 'l'},
		{-11, -3, 'l'},
		{-11, -2, 'l'},
		{-11, -1, 'l'},
		{-11, 0, 'l'},
		{-11, 1, 'l'},
	} {
		p := ChunkPos{s.x, s.z}
		switch s.phase {
		case 'f':
			g.DecorateInOrder([]ChunkPos{p})
		case 's':
			g.PrimeLightSources(p)
		case 'i', 'l':
			g.ReplayLight(p, s.phase == 'l')
		}
	}
	c := newChunk(-13, -1)
	g.Generate(c)
	if got := registry.BlockOf(c.BlockAt(-199&15, 70, -16&15)); got != registry.BlockAir {
		t.Fatalf("(-199,70,-16): got %s, want air", got.Name())
	}
}
