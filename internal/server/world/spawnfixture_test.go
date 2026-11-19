package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type spawnParityGenerator struct {
	*FlatGenerator
	water bool
}

func (g spawnParityGenerator) Generate(c *Chunk) {
	floor := int32(30)
	if g.water {
		floor = 55
	}
	c.Fill(worldMinY, floor, registry.BlockGrassBlock.DefaultState())
	if g.water {
		c.Fill(floor, 63, registry.BlockWater.DefaultState())
	}
	biomes := []registry.Biome{registry.BiomePlains, registry.BiomeSwamp, registry.BiomeTaiga, registry.BiomeBambooJungle, registry.BiomeJungle, registry.BiomeWarpedForest}
	if g.water {
		biomes = []registry.Biome{registry.BiomeRiver, registry.BiomeOcean, registry.BiomeWarmOcean, registry.BiomeLushCaves}
	}
	for x := int32(0); x < 16; x += 4 {
		for z := int32(0); z < 16; z += 4 {
			for y := int32(worldMinY); y < worldMinY+worldHeight; y += 4 {
				i := (c.x*4 + x/4 + c.z*4 + z/4 + y/4) % int32(len(biomes))
				if i < 0 {
					i += int32(len(biomes))
				}
				c.SetCellBiome(x, y, z, int32(biomes[i]))
			}
		}
	}
	ids := []string{"minecraft:swamp_hut", "minecraft:monument", "minecraft:pillager_outpost", "minecraft:ancient_city", "minecraft:trial_chambers"}
	id := ids[int(uint32(c.x+c.z)%uint32(len(ids)))]
	bb := nbt.IntArray{c.x*16 + 8, floor, c.z * 16, c.x*16 + 18, floor + 8, c.z*16 + 10}
	c.setStructureStart(id, structureStartNBT(id, c.x, c.z, []nbt.Tag{nbt.Compound{"BB": bb}}))
	c.setStructureReferences(id, []int64{packChunk(c.x, c.z)})
}
func (g spawnParityGenerator) SeaLevel() int32 { return 63 }

// SpawnParityFixture supplies real chunks and saved structure references to the external spawn parity test.
func SpawnParityFixture(t testing.TB, water bool) *World {
	t.Helper()
	w := newTestWorld(t, spawnParityGenerator{NewFlatGenerator(), water})
	w.SetClockTime("minecraft:overworld", 18000)
	for x := int32(-2); x <= 3; x++ {
		for z := int32(-1); z <= 1; z++ {
			pos := ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			t.Cleanup(func() { w.Release(pos) })
		}
	}
	return w
}
