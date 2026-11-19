package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func mushroomRegion() *decorRegion {
	g := NewNetherGenerator(0)
	r := &decorRegion{src: g, g: g, minY: 0, height: netherGenHeight, center: newChunk(0, 0), rng: levelgen.NewWorldgenRandom(0)}
	empty := &shapedTerrain{}
	for i := range empty.fill {
		empty.fill[i] = airState
	}
	for i := range r.near {
		r.near[i] = empty
	}
	return r
}

func TestNetherMushroomFeaturesMarkForFull(t *testing.T) {
	t.Parallel()
	r := mushroomRegion()
	p := [3]int32{5, 100, 5}
	r.set(5, 99, 5, r.g.netherrackState)
	r.set(6, 100, 5, registry.BlockGlowstone.DefaultState())
	for _, f := range r.g.placed {
		if f.feature.Name == "minecraft:brown_mushroom_nether" {
			f.place(r, p)
		}
	}
	if r.get(5, 100, 5) != r.g.brownMushroomState {
		t.Fatal("FEATURES rejected illuminated mushroom")
	}
	own := r.protoAt(5, 5)
	if len(own.shapes) != 1 || own.shapes[0] != (posKey{5, 100, 5}) {
		t.Fatalf("post-processing marks: %v", own.shapes)
	}
	r.reconnectShapes(own)
	if r.get(5, 100, 5) != airState {
		t.Fatal("FULL retained bright mushroom")
	}
	own.shapes = nil
	for _, f := range r.g.placed {
		if f.feature.Name == "minecraft:patch_fire" {
			f.place(r, p)
		}
	}
	if r.get(5, 100, 5) != r.g.fireState || len(own.shapes) != 0 {
		t.Fatal("fire should place without a post-processing mark")
	}
}

func TestNetherMushroomFullSurvival(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		support  registry.Block
		source   registry.Block
		distance int32
		keep     bool
	}{
		{"light13", registry.BlockNetherrack, registry.BlockGlowstone, 2, false},
		{"light12", registry.BlockNetherrack, registry.BlockTorch, 2, true},
		{"override", registry.BlockCrimsonNylium, registry.BlockGlowstone, 1, true},
		{"missing support", registry.BlockAir, registry.BlockAir, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mushroomRegion()
			r.set(5, 99, 5, tc.support.DefaultState())
			r.set(5+tc.distance, 100, 5, tc.source.DefaultState())
			r.set(5, 100, 5, r.g.redMushroomState)
			r.markPost(5, 100, 5)
			r.reconnectShapes(r.protoAt(5, 5))
			if got := r.get(5, 100, 5) != airState; got != tc.keep {
				t.Fatalf("survives=%v, want %v", got, tc.keep)
			}
		})
	}
}

func TestNetherMushroomLightPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source posKey
		walls  []posKey
		bright bool
	}{
		{"target", posKey{15, 100, 5}, nil, true},
		{"one edge across chunk", posKey{16, 100, 5}, nil, true},
		{"two edges across chunk", posKey{17, 100, 5}, nil, true},
		{"two edge turn", posKey{16, 101, 5}, []posKey{{16, 100, 5}}, true},
		{"blocked straight", posKey{17, 100, 5}, []posKey{{16, 100, 5}}, false},
		{"blocked turn", posKey{16, 101, 5}, []posKey{{16, 100, 5}, {15, 101, 5}}, false},
		{"three edges", posKey{18, 100, 5}, nil, false},
		{"below height", posKey{15, -1, 5}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mushroomRegion()
			r.set(tc.source[0], tc.source[1], tc.source[2], registry.BlockGlowstone.DefaultState())
			for _, p := range tc.walls {
				r.set(p[0], p[1], p[2], registry.BlockStone.DefaultState())
			}
			y := int32(100)
			if tc.name == "below height" {
				y = 0
			}
			if got := r.finalLightAtLeast13(15, y, 5); got != tc.bright {
				t.Fatalf("bright=%v, want %v", got, tc.bright)
			}
		})
	}
}

func TestNetherMushroomOverwrittenMark(t *testing.T) {
	t.Parallel()
	r := mushroomRegion()
	r.set(5, 100, 5, r.g.redMushroomState)
	r.markPost(5, 100, 5)
	r.set(5, 100, 5, torchBlock.DefaultState())
	r.reconnectShapes(r.protoAt(5, 5))
	if r.get(5, 100, 5) != airState {
		t.Fatal("overwritten torch did not receive its support check")
	}
	r.set(5, 100, 5, registry.BlockStone.DefaultState())
	r.reconnectShapes(r.protoAt(5, 5))
	if r.get(5, 100, 5) != registry.BlockStone.DefaultState() {
		t.Fatal("overwritten solid block changed")
	}
}

func TestNetherMushroomOverrideNonSolid(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {"minecraft:overrides_mushroom_light_requirement": {Replace: true, Entries: []registry.PackTagEntry{{ID: "minecraft:glass"}}}}})
	r := mushroomRegion()
	r.set(5, 99, 5, registry.BlockGlass.DefaultState())
	r.set(6, 100, 5, registry.BlockGlowstone.DefaultState())
	for _, f := range r.g.placed {
		if f.feature.Name == "minecraft:red_mushroom_nether" {
			f.place(r, [3]int32{5, 100, 5})
		}
	}
	r.reconnectShapes(r.protoAt(5, 5))
	if r.get(5, 100, 5) != r.g.redMushroomState {
		t.Fatal("tag override did not bypass light and solid rendering")
	}
}

func TestNetherMushroomLightSealedFace(t *testing.T) {
	t.Parallel()
	r := mushroomRegion()
	r.set(5, 102, 5, registry.BlockGlowstone.DefaultState())
	slab, _ := registry.WithProp(registry.BlockStoneSlab.DefaultState(), registry.PropType, "top")
	r.set(5, 101, 5, slab)
	if registry.LightDampening(slab) != 0 {
		t.Fatal("expected a shape-occluding transparent slab")
	}
	if r.finalLightAtLeast13(5, 100, 5) {
		t.Fatal("light crossed the slab's sealed face")
	}
}
