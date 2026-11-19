package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

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
