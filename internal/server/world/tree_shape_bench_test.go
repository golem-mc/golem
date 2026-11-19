package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

const forestChunkSettings = `{"biome":"minecraft:forest","features":true,"structure_overrides":[],"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":3},{"block":"minecraft:grass_block","height":1}]}`

// BenchmarkForestChunkDecorate measures one forest chunk's features over cached
// flat terrain, keeping terrain generation and feature compilation out of timing.
func BenchmarkForestChunkDecorate(b *testing.B) {
	g := NewFlatOverworldGeneratorFor(0, forestChunkSettings)
	decorateForestChunk(g)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		decorateForestChunk(g)
	}
}

func decorateForestChunk(g *OverworldGenerator) *decorRegion {
	g.protos = decorProtoStore{}
	c := newChunk(0, 0)
	g.shape(c)
	r := newDecorRegion(g, c, worldMinY, worldHeight)
	r.biomeCells = &cellReader{g: g}
	r.decorateOverworld(0, 0)
	return r
}

func TestForestChunkDecorateFixture(t *testing.T) {
	g := NewFlatOverworldGeneratorFor(0, forestChunkSettings)
	// Repeat with warm terrain and cleared protos, as in the benchmark.
	for range 2 {
		r := decorateForestChunk(g)
		logs := 0
		for x := range int32(16) {
			for z := range int32(16) {
				for y := int32(worldMinY + 5); y < worldMinY+40; y++ {
					if registry.BlockOf(r.get(x, y, z)).InTag(registry.TagIDLogs) {
						logs++
					}
				}
			}
		}
		if logs == 0 {
			t.Fatal("forest decoration placed no logs")
		}
	}
}
