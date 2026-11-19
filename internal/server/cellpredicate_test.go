package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestCompiledBlockPredicateMatchesKinds(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	w.SetBlock(8, 80, 8, registry.BlockWater.DefaultState())
	w.SetBlock(9, 80, 8, registry.BlockStone.DefaultState())
	w.SetBlock(8, 81, 8, registry.BlockAir.DefaultState())
	s := &Server{}
	never := func(x, y, z int32) bool { return false }
	water := &registry.EnchBlockPredicate{Kind: "matching_blocks", Blocks: []registry.Block{registry.BlockWater}}
	fluid := &registry.EnchBlockPredicate{Kind: "matching_fluids", Names: []string{"minecraft:water"}}
	airAbove := &registry.EnchBlockPredicate{Kind: "matching_blocks", Offset: [3]int32{0, 1, 0}, Blocks: []registry.Block{registry.BlockAir}}
	solid := &registry.EnchBlockPredicate{Kind: "solid"}
	height := &registry.EnchBlockPredicate{Kind: "height_range", Bottom: registry.EnchAnchor{Kind: "absolute", Value: 80}, Top: registry.EnchAnchor{Kind: "absolute", Value: 80}}
	all := &registry.EnchBlockPredicate{Kind: "all_of", Terms: []*registry.EnchBlockPredicate{airAbove, water, fluid}}
	for _, tc := range []struct {
		name    string
		p       *registry.EnchBlockPredicate
		x, y, z int32
		want    bool
	}{
		{"nil", nil, 0, 0, 0, true},
		{"all_of water under air", all, 8, 80, 8, true},
		{"all_of on stone", all, 9, 80, 8, false},
		{"any_of", &registry.EnchBlockPredicate{Kind: "any_of", Terms: []*registry.EnchBlockPredicate{solid, water}}, 8, 80, 8, true},
		{"any_of none", &registry.EnchBlockPredicate{Kind: "any_of", Terms: []*registry.EnchBlockPredicate{solid, water}}, 10, 80, 8, false},
		{"not solid", &registry.EnchBlockPredicate{Kind: "not", Terms: []*registry.EnchBlockPredicate{solid}}, 9, 80, 8, false},
		{"solid", solid, 9, 80, 8, true},
		{"height in", height, 8, 80, 8, true},
		{"height out", height, 8, 81, 8, false},
		{"unknown kind", &registry.EnchBlockPredicate{Kind: "true"}, 8, 80, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.blockPredicateTestIn(tc.p, w, tc.x, tc.y, tc.z, never); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
