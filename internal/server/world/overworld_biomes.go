package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// setBiomeSource builds the overworld's multi-noise biome source for the seed.
// A flat world's source answers biome 0 everywhere and every entry of biomeIDs
// names the flat biome, so each reader of an index reads that biome. A
// single-biome world's source answers plains everywhere.
// vanilla: FixedBiomeSource, WorldPresets.Bootstrap (SINGLE_BIOME_SURFACE)
func (g *OverworldGenerator) setBiomeSource(_ int64) {
	if g.flat != nil {
		g.biomeSource = terrain.NewFixedOverworldBiomeSource(0)
		for i := range g.biomeIDs {
			g.biomeIDs[i] = g.flat.biome
		}
		return
	}
	if g.preset == presetSingleBiome {
		g.biomeSource = terrain.NewFixedOverworldBiomeSourceFor(g.state, g.singleBiome())
	} else {
		g.biomeSource = terrain.NewOverworldBiomeSource(g.state)
	}
	for i := range g.biomeIDs {
		g.biomeIDs[i] = int32(overworldBiomeIDs[i])
	}
}

// singleBiome is the biome of a single-biome world: the one a plugin chose,
// else plains.
func (g *OverworldGenerator) singleBiome() terrain.OverworldBiome {
	if g.hasSingle {
		return g.single
	}
	return terrain.OverworldPlains
}

// fixedBiome is the one biome a flat or single-biome world's source answers,
// false for the multi-noise overworld.
func (g *OverworldGenerator) fixedBiome() (registry.Biome, bool) {
	switch {
	case g.flat != nil:
		return registry.Biome(g.flat.biome), true
	case g.preset == presetSingleBiome:
		return overworldBiomeIDs[g.singleBiome()], true
	}
	return 0, false
}

// possibleBiomes is every biome the biome source can answer.
// vanilla: BiomeSource.possibleBiomes
func (g *OverworldGenerator) possibleBiomes() []registry.Biome {
	if b, ok := g.fixedBiome(); ok {
		return []registry.Biome{b}
	}
	return overworldBiomeIDs[:]
}

// biomeGrid is a chunk's noise biomes by quart: x, then y from the bottom,
// then z, as FillChunk visits them.
type biomeGrid [4][96][4]terrain.OverworldBiome

// computeBiomes fills a chunk's grid and returns the set of its biomes.
func (g *OverworldGenerator) computeBiomes(cx, cz int32, grid *biomeGrid) (mask uint64) {
	g.biomeFills.Add(1)
	g.biomeSource.FillChunk(cx, cz, func(x, y, z int32, b terrain.OverworldBiome) {
		grid[x][y][z] = b
		mask |= 1 << b
	})
	return mask
}

// assignBiomes stores a computed grid in every cell of the chunk.
// vanilla: ChunkAccess.fillBiomesFromNoise
func (g *OverworldGenerator) assignBiomes(c *Chunk, grid *biomeGrid) {
	for x := range grid {
		for y := range grid[x] {
			for z, b := range grid[x][y] {
				c.SetCellBiome(int32(x)*4, worldMinY+int32(y)*4, int32(z)*4, g.biomeIDs[b])
			}
		}
	}
	for i := range c.sections {
		c.sections[i].settleBiomes()
	}
}

// overworldBiomeIDs is the registry biome of each terrain.OverworldBiome.
var overworldBiomeIDs = func() (ids [terrain.OverworldBiomeCount]registry.Biome) {
	for i := range ids {
		ids[i] = registry.Biome(registry.MustID("minecraft:worldgen/biome", "minecraft:"+terrain.OverworldBiome(i).Name()))
	}
	return ids
}()

// PossibleBiomes is every biome the overworld's parameter list names.
func (g *OverworldGenerator) PossibleBiomes() []string {
	if g.flat != nil {
		return []string{g.flat.biomeName}
	}
	var out []string
	for _, b := range g.possibleBiomes() {
		out = append(out, b.Name())
	}
	return out
}

// NewBiomeProbe reads the overworld's climate biome at a quart.
// vanilla: MultiNoiseBiomeSource.getNoiseBiome
func (g *OverworldGenerator) NewBiomeProbe() func(qx, qy, qz int32) string {
	probe := g.newBiomeIDProbe()
	return func(qx, qy, qz int32) string { return probe(qx, qy, qz).Name() }
}

// newBiomeIDProbe is NewBiomeProbe answering with the registry ID.
func (g *OverworldGenerator) newBiomeIDProbe() func(qx, qy, qz int32) registry.Biome {
	sampler := g.biomeSource.NewSampler()
	return func(qx, qy, qz int32) registry.Biome {
		return registry.Biome(g.biomeIDs[sampler.At(qx, qy, qz)])
	}
}
