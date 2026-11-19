package entity

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// WardenSpawnWorld supplies terrain, border and collision checks for a triggered warden.
type WardenSpawnWorld interface {
	BlockAt(x, y, z int32) uint16
	InBorder(x, z int32) bool
	BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool
}

// FindWardenSpawnSpot tries twenty columns within five blocks of a shrieker.
// vanilla: SculkShriekerBlockEntity.trySummonWarden, SpawnUtil.trySpawnMob
func FindWardenSpawnSpot(w WardenSpawnWorld, x, y, z int32, intn func(int) int, bodies []SpawnBody) (int32, int32, int32, bool) {
	for range 20 {
		cx, cz := x+int32(intn(11)-5), z+int32(intn(11)-5)
		if !w.InBorder(cx, cz) {
			continue
		}
		cy := y + 6
		above := w.BlockAt(cx, cy, cz)
		for range 13 {
			cy--
			here := w.BlockAt(cx, cy, cz)
			// vanilla: SpawnUtil.Strategy.ON_TOP_OF_COLLIDER (leaves are allowed)
			if world.CollisionTop(above) == 0 && registry.CollisionTopFull(here) {
				fy := cy + 1
				width, height := KindDims(TypeWarden)
				fx, fz := float64(cx)+0.5, float64(cz)+0.5
				wet := blocklogic.FluidOf(w.BlockAt(cx, fy, cz)).Kind != blocklogic.FluidNone
				emergingBox := spawnBox(TypeWarden, fx, float64(fy), fz)
				emergingBox.MaxY = float64(fy) + 1
				// Only block collision uses the standing box; liquid and bodies use the emerging box.
				// vanilla: Warden.checkSpawnObstruction, Mob.checkSpawnObstruction
				if !wet && !w.BoxCollides(fx-width/2, float64(fy), fz-width/2, fx+width/2, float64(fy)+height, fz+width/2) &&
					!boxOverlapsBody(bodies, emergingBox) {
					return cx, fy, cz, true
				}
				break
			}
			above = here
		}
	}
	return 0, 0, 0, false
}
