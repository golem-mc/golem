package entity

import (
	"github.com/golem-mc/golem/registry"
)

type placementDiceWorld struct {
	spawnTestWorld
	ground uint16
}

func (w placementDiceWorld) BlockAt(x, y, z int32) uint16 {
	if y == 63 {
		return w.ground
	}
	if y < 63 {
		return registry.BlockStone.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
func (w placementDiceWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}
