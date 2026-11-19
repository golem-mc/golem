package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func TestFossilEmptyCorners(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	box := jbox{1, 1, 1, 3, 3, 3}
	if got := r.countEmptyCorners(box); got != 0 {
		t.Fatalf("solid corners: got %d, want 0", got)
	}
	for _, b := range []struct {
		pos   [3]int32
		block registry.Block
	}{
		{[3]int32{1, 1, 1}, registry.BlockAir},
		{[3]int32{1, 1, 3}, registry.BlockCaveAir},
		{[3]int32{1, 3, 1}, registry.BlockWater},
		{[3]int32{1, 3, 3}, registry.BlockLava},
		// Non-solid blocks other than air and the two liquids do not count.
		{[3]int32{3, 1, 1}, registry.BlockOakLeaves},
	} {
		r.set(b.pos[0], b.pos[1], b.pos[2], b.block.DefaultState())
	}
	if got := r.countEmptyCorners(box); got != 4 {
		t.Fatalf("air and liquid corners: got %d, want 4", got)
	}
	// BoundingBox.forAllCorners visits all eight even when coordinates coincide.
	if got := r.countEmptyCorners(jbox{1, 1, 1, 1, 1, 1}); got != 8 {
		t.Fatalf("degenerate air box: got %d, want 8", got)
	}
}

func TestFossilLayerProcessors(t *testing.T) {
	world := newGoldenTerrain()
	layer := loadFossilLayer(nil, "minecraft:fossil_diamonds")
	layer.templates = []*Template{{
		Size: [3]int32{3, 1, 1},
		Palettes: [][]TemplateBlock{{
			{Pos: [3]int32{0, 0, 0}, State: registry.BlockCoalOre.DefaultState()},
			{Pos: [3]int32{1, 0, 0}, State: registry.BlockCoalOre.DefaultState()},
			{Pos: [3]int32{2, 0, 0}, State: registry.BlockCoalOre.DefaultState()},
		}},
	}}
	kept := 0
	for seed := range int64(100) {
		clear(world.protos.mem)
		r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
		r.rng.SetSeed(seed)
		r.set(1, 1, 1, registry.BlockBedrock.DefaultState())
		// The third block is outside the placement bound. Both it and the
		// protected block must still draw their rot roll from the shared stream.
		layer.place(r, 0, [3]int32{0, 1, 1}, RotateNone, jbox{0, 0, 0, 1, 3, 3})
		random := levelgen.NewWorldgenRandom(seed)
		random.NextIntN(1) // palette selection
		want := registry.BlockStone.DefaultState()
		if random.NextFloat() <= 0.1 {
			want = registry.BlockDeepslateDiamondOre.DefaultState()
			kept++
		}
		random.NextFloat()
		random.NextFloat()
		if got := r.get(0, 1, 1); got != want {
			t.Fatalf("seed %d: overlay got %d, want %d", seed, got, want)
		}
		if r.get(1, 1, 1) != registry.BlockBedrock.DefaultState() || r.get(2, 1, 1) != registry.BlockStone.DefaultState() {
			t.Fatalf("seed %d: wrote a protected or out-of-bounds block", seed)
		}
		if r.rng.NextLong() != random.NextLong() {
			t.Fatalf("seed %d: processors drew from the wrong stream", seed)
		}
	}
	if kept == 0 || kept == 100 {
		t.Fatalf("rot did not exercise both outcomes: kept %d", kept)
	}
}
