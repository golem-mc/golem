package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// EndGatewayCooldownTicks is how long a gateway refuses the next entity after
// one touches it.
// vanilla: TheEndGatewayBlockEntity.COOLDOWN_TIME
const EndGatewayCooldownTicks = 40

// EndGatewayBeamInterval is how many ticks of age pass between the beams an
// idle gateway sends.
// vanilla: TheEndGatewayBlockEntity.ATTENTION_INTERVAL
const EndGatewayBeamInterval = 2400

// EndGatewayReach is how many blocks around the exit the landing search
// reads, so the caller holds those columns loaded.
// vanilla: TheEndGatewayBlockEntity.findExitPosition
const EndGatewayReach int32 = 5

// EndGatewayState is the end gateway block's only state.
var EndGatewayState = world.EndGatewayState

// EndGatewayExit reads the exit a gateway's block entity data names and
// reports it as a block position, with whether the exit is exact. A missing
// or out-of-bounds exit_portal reads as no exit.
// vanilla: TheEndGatewayBlockEntity.loadAdditional, Level.isInSpawnableBounds
func EndGatewayExit(tag nbt.Compound) (x, y, z int32, exact, ok bool) {
	pos, found := exitPortal(tag)
	if !found {
		return 0, 0, 0, false, false
	}
	if pos[0] < -30000000 || pos[0] >= 30000000 || pos[2] < -30000000 || pos[2] >= 30000000 {
		return 0, 0, 0, false, false
	}
	flag, _ := nbt.Get[nbt.Byte](tag, "ExactTeleport")
	return pos[0], pos[1], pos[2], flag != 0, true
}

// exitPortal reads exit_portal as an int array or a list of ints.
// vanilla: NbtOps.getIntStream
func exitPortal(tag nbt.Compound) ([]int32, bool) {
	if arr, ok := nbt.Get[nbt.IntArray](tag, "exit_portal"); ok {
		return arr, len(arr) == 3
	}
	list, ok := nbt.Get[nbt.List](tag, "exit_portal")
	if !ok || len(list.Items) != 3 {
		return nil, false
	}
	out := make([]int32, 3)
	for i, it := range list.Items {
		v, ok := it.(nbt.Int)
		if !ok {
			return nil, false
		}
		out[i] = int32(v)
	}
	return out, true
}

// EndGatewayLanding is where a gateway sends its entity: the exit block's
// bottom centre when the teleport is exact, else the cell above the tallest
// full block within 5 of two above the exit. The world must hold the chunks
// around the exit. minY and maxY bound the dimension's blocks.
// vanilla: TheEndGatewayBlockEntity.getPortalPosition, findExitPosition
func EndGatewayLanding(w BlockReader, x, y, z int32, exact bool, minY, maxY int32) (px, py, pz float64) {
	if !exact {
		bx, by, bz := EndGatewayTallest(w, x, y+2, z, EndGatewayReach, false, minY, maxY)
		x, y, z = bx, by+1, bz
	}
	return float64(x) + 0.5, float64(y), float64(z) + 0.5
}

// EndGatewayTallest is findTallestBlock: the highest full collision block
// within dist columns of (ax, az), the first found winning a tie, or the
// centre itself when there is none. Bedrock and the centre column count only
// with allowBedrock. A column's scan stops above the best so far, and never
// tests the dimension's lowest layer.
// vanilla: TheEndGatewayBlockEntity.findTallestBlock
func EndGatewayTallest(w BlockReader, ax, ay, az, dist int32, allowBedrock bool, minY, maxY int32) (int32, int32, int32) {
	var best [3]int32
	found := false
	for xd := -dist; xd <= dist; xd++ {
		for zd := -dist; zd <= dist; zd++ {
			if xd == 0 && zd == 0 && !allowBedrock {
				continue
			}
			floor := minY
			if found {
				floor = best[1]
			}
			for y := maxY; y > floor; y-- {
				state := w.BlockAt(ax+xd, y, az+zd)
				if !registry.CollisionShapeFull(state) {
					continue
				}
				if !allowBedrock && registry.BlockOf(state) == registry.BlockBedrock {
					continue
				}
				best, found = [3]int32{ax + xd, y, az + zd}, true
				break
			}
		}
	}
	if !found {
		return ax, ay, az
	}
	return best[0], best[1], best[2]
}

// EndGatewayTentative is where the search for an exit starts: 1024 blocks out
// along the gateway's direction from the origin, stepped back by 16 past
// chunks that hold blocks, then forward past empty ones, each up to 16 times.
// empty reports whether the chunk over a point is empty.
// vanilla: TheEndGatewayBlockEntity.findExitPortalXZPosTentative
func EndGatewayTentative(gx, gz int32, empty func(x, z float64) bool) (x, z float64) {
	dx, dz := float64(gx), float64(gz)
	if length := math.Sqrt(dx*dx + dz*dz); length < 1e-5 {
		dx, dz = 0, 0
	} else {
		dx, dz = dx/length, dz/length
	}
	x, z = dx*1024, dz*1024
	for limit := 16; !empty(x, z) && limit > 0; limit-- {
		x, z = x+dx*-16, z+dz*-16
	}
	for limit := 16; empty(x, z) && limit > 0; limit-- {
		x, z = x+dx*16, z+dz*16
	}
	return x, z
}

// EndChunkTop is the highest block of a chunk's top filled section, and
// whether the chunk has any block that is not air.
// vanilla: LevelChunk.getHighestSectionPosition, getHighestFilledSectionIndex
func EndChunkTop(w BlockReader, cx, cz, minY, maxY int32) (top int32, filled bool) {
	for y := maxY; y >= minY; y-- {
		for x := cx * 16; x < cx*16+16; x++ {
			for z := cz * 16; z < cz*16+16; z++ {
				if !isAirBlock(w.BlockAt(x, y, z)) {
					return (y-minY)/16*16 + minY + 15, true
				}
			}
		}
	}
	return 0, false
}

// isAirBlock is isAir: air, cave air or void air.
func isAirBlock(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockAir, registry.BlockCaveAir, registry.BlockVoidAir:
		return true
	}
	return false
}

// EndGatewayValidSpawn is the end stone of the chunk, from y 30 to top, with
// two cells above it not full blocks, closest to the origin; the first of a
// tie in x, y, z order wins.
// vanilla: TheEndGatewayBlockEntity.findValidSpawnInChunk
func EndGatewayValidSpawn(w BlockReader, cx, cz, top int32) (x, y, z int32, ok bool) {
	best := math.MaxFloat64
	for bz := cz * 16; bz < cz*16+16; bz++ {
		for by := int32(30); by <= top; by++ {
			for bx := cx * 16; bx < cx*16+16; bx++ {
				if registry.BlockOf(w.BlockAt(bx, by, bz)) != registry.BlockEndStone ||
					registry.CollisionShapeFull(w.BlockAt(bx, by+1, bz)) ||
					registry.CollisionShapeFull(w.BlockAt(bx, by+2, bz)) {
					continue
				}
				fx, fy, fz := float64(bx)+0.5, float64(by)+0.5, float64(bz)+0.5
				if d := fx*fx + fy*fy + fz*fz; !ok || d < best {
					best, x, y, z, ok = d, bx, by, bz, true
				}
			}
		}
	}
	return x, y, z, ok
}
