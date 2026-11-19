package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// PortalArrivalPosition maps the entry offset and clears blocks around the exit.
// vanilla: NetherPortalBlock.createDimensionTransition, PortalShape.findCollisionFreePosition
func PortalArrivalPosition(from rules.BlockReader, to World, entry, exit world.SpawnPos, pos [3]float64, width, height float64, centered bool) [3]float64 {
	at := rules.NetherPortalPosition(from, to, entry, exit, pos, width, height, centered)
	if width > 4 || height > 4 {
		return at
	}
	halfHeight := height / 2
	center := [3]float64{at[0], at[1] + halfHeight, at[2]}
	allowed := AABB{at[0] - width/2, center[1], at[2] - width/2, at[0] + width/2, center[1] + 1, at[2] + width/2}.Inflate(1e-6)
	if free, ok := findFreePosition(to, allowed, center, width, height, width); ok {
		return [3]float64{free[0], free[1] - halfHeight, free[2]}
	}
	return at
}
