package entity

import (
	"github.com/golem-mc/golem/registry"
)

type spawnBoundaryWorld struct {
	spawnTestWorld
	water bool
}

func (w spawnBoundaryWorld) Biome(x, y, z int32) registry.Biome {
	if w.water {
		if x&16 == 0 {
			return registry.BiomeRiver
		}
		return registry.BiomeOcean
	}
	if x&16 == 0 {
		return registry.BiomePlains
	}
	return registry.BiomeSwamp
}
func (w spawnBoundaryWorld) BlockAt(x, y, z int32) uint16 {
	floor := int32(30)
	if w.water {
		floor = 55
	}
	if y < floor {
		return registry.BlockGrassBlock.DefaultState()
	}
	if w.water && y < 59 {
		return registry.BlockWater.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
func (w spawnBoundaryWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}
func (w spawnBoundaryWorld) StructureContains(x, y, z int32, id string, piece bool) bool {
	return id == "minecraft:swamp_hut" && piece && x >= 8 && x <= 24 && y >= 30 && y <= 32
}
