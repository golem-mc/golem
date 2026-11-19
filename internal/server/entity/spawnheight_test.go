package entity

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type spawnHeightGenerator struct{ surface int32 }

func (g spawnHeightGenerator) Generate(c *world.Chunk) {
	c.Fill(-64, g.surface+1, registry.BlockStone.DefaultState())
}
func (spawnHeightGenerator) SeaLevel() int32 { return 63 }
