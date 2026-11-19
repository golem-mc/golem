package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestOverworldDecorateInOrder(t *testing.T) {
	t.Parallel()
	var forward []ChunkPos
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			forward = append(forward, ChunkPos{X: x, Z: z})
		}
	}
	backward := slices.Clone(forward)
	slices.Reverse(backward)
	var results [2][]uint16
	for i, seq := range [][]ChunkPos{forward, backward} {
		g := NewOverworldGenerator(2)
		g.DecorateInOrder(seq[:1])
		for pos, p := range g.protos.mem {
			if p.decorated && pos != seq[0] {
				t.Fatalf("single task decorated neighbour %v", pos)
			}
		}
		g.DecorateInOrder(seq)
		for _, pos := range seq {
			p := g.protos.lookup(pos)
			if p == nil || !p.decorated || p.final {
				t.Fatalf("order %d chunk %v: want decorated, unfinished proto", i, pos)
			}
		}
		clock := g.protos.clock
		g.DecorateInOrder(backward)
		if g.protos.clock != clock {
			t.Fatal("repeated chunks ran decoration again")
		}
		c := newChunk(0, 0)
		g.Generate(c)
		for pos, p := range g.protos.mem {
			if p.decorated && !slices.Contains(forward, pos) {
				t.Fatalf("Generate decorated extra chunk %v", pos)
			}
			if p.final && pos != (ChunkPos{}) {
				t.Fatalf("Generate finished extra chunk %v", pos)
			}
		}
		results[i] = chunkBlocks(c)
	}
	g := NewOverworldGenerator(2)
	c := newChunk(0, 0)
	g.Generate(c)
	if !slices.Equal(results[0], chunkBlocks(c)) {
		t.Fatal("replaying the default 3x3 order differs from normal generation")
	}
	different := 0
	unexpected := map[[2]registry.Block]int{}
	for i, a := range results[0] {
		b := results[1][i]
		if a == b {
			continue
		}
		x, y := registry.BlockOf(a), registry.BlockOf(b)
		isTree := func(b registry.Block) bool {
			return b.InTag(registry.TagIDLogs) || b.IsLeaves() || b == registry.BlockLeafLitter
		}
		seaPlant := (seaFloorBlocks[x] || x == registry.BlockWater) && (seaFloorBlocks[y] || y == registry.BlockWater)
		if !orderDependent(a, b) && !isTree(x) && !isTree(y) && !seaPlant {
			unexpected[[2]registry.Block{x, y}]++
		}
		different++
	}
	for pair, count := range unexpected {
		t.Errorf("%d cells differ outside overlapping ores, blobs, trees and sea plants: %s / %s", count, pair[0].Name(), pair[1].Name())
	}
	if different == 0 {
		t.Fatal("opposite decoration orders produced identical blocks; no overlap exercised")
	}
}
