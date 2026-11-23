package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The spawn search: PlayerSpawnFinder's walk over the respawn_radius square
// around a suggestion for a block a body can stand on, with the height fixup as
// fallback when the square offers nothing. A fresh join and a respawn without a
// spawn point both end here; a saved position, bed or /spawnpoint is taken as
// it stands.

// RespawnColumn reports where a body may stand in the column
// (PlayerSpawnFinder.getLevelRespawnPos): above the highest full-topped block
// below the motion-blocking height, refusing the column when a fluid is in the
// scan's way or it reads as open water (the WORLD_SURFACE top at or below the
// motion-blocking one but above the solid floor, how water looks to the three
// heightmaps). The chunk is loaded for the scan, as vanilla's ticket does, and
// released.
//
// The three heightmaps come from one descending walk over the column instead of
// three kept maps. Motion and the solid floor use blocksMotion; pure fluids
// also count toward motion. Ceilinged worlds (cave worlds, nether, end)
// would use the same surface heightmaps rather than a
// generator spawn height, since only the overworld reaches a spawn search here.
func (w *World) RespawnColumn(x, z int32) (SpawnPos, bool) {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	return respawnColumn(chunk, x, z, w.dim.minY, w.dim.height)
}

// vanilla: PlayerSpawnFinder.getLevelRespawnPos
func respawnColumn(chunk *Chunk, x, z, minY, height int32) (SpawnPos, bool) {
	top := minY + height - 1
	empty := minY - 1
	var motion, solid, surface = empty, empty, empty
	for y := top; y >= minY; y-- {
		state := chunk.BlockAt(x&15, y, z&15)
		if state == airState {
			continue
		}
		if blocklogic.FluidOf(state).Pure {
			if motion == empty {
				motion = y
			}
			if surface == empty {
				surface = y
			}
			continue
		}
		blocksMotion := heightmapSolid(state)
		if motion == empty && blocksMotion {
			motion = y
		}
		if surface == empty {
			surface = y
		}
		if solid == empty && blocksMotion {
			solid = y
		}
	}

	if motion < minY {
		return SpawnPos{}, false
	}
	if surface <= motion && surface > solid {
		return SpawnPos{}, false
	}
	for y := motion + 1; y >= minY; y-- {
		state := chunk.BlockAt(x&15, y, z&15)
		if blocklogic.FluidOf(state).Pure {
			break
		}
		if !faceFullUp(state) {
			continue
		}
		return SpawnPos{X: x, Y: y + 1, Z: z}, true
	}
	return SpawnPos{}, false
}

// faceFullUp reports whether a block's collision fills its top face.
// vanilla: Block.isFaceFull
func faceFullUp(state uint16) bool {
	return !blocklogic.FluidOf(state).Pure && registry.CollisionTopFull(state)
}

// SpawnBoxClear reports whether the box of a body standing at the bottom centre
// of the block (half width each way, height up from the floor) meets no
// collision or fluid: the spawn search's CollisionGetter.noCollision with
// fluids counted. Cells are read like the mob spawn's box check, floor bounds
// both ways, which for a player is the column and the block above. Mobs and
// players the vanilla check also avoids are the caller's to check; the world
// knows only the ground.
func (w *World) SpawnBoxClear(sp SpawnPos, width, height float64) bool {
	cx, cz := float64(sp.X)+0.5, float64(sp.Z)+0.5
	minX := int32(math.Floor(cx - width/2))
	maxX := int32(math.Floor(cx + width/2))
	minY := sp.Y
	maxY := int32(float64(sp.Y) + height)
	minZ := int32(math.Floor(cz - width/2))
	maxZ := int32(math.Floor(cz + width/2))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				state := w.BlockAt(x, y, z)
				if blocklogic.FluidOf(state).Pure {
					return false
				}
				if !registry.BlockOf(state).NoCollision() {
					return false
				}
			}
		}
	}
	return true
}

// FixupSpawnHeight walks a suggestion to a standable place
// (PlayerSpawnFinder.fixupSpawnHeight): up while buried or under water, back
// down through clear spots to the lowest, then up the one step that stands the
// body on the ground. It is the search's fallback and what an adventure world's
// join gets instead of a search. bodiesClear, when not nil, is the entity half
// of the box check, so a spot inside a boat counts as buried.
func (w *World) FixupSpawnHeight(spawn SpawnPos, bodiesClear func(SpawnPos) bool) SpawnPos {
	pos := ChunkPos{X: ChunkOf(float64(spawn.X)), Z: ChunkOf(float64(spawn.Z))}
	chunk, _ := w.Acquire(pos)
	defer w.Release(pos)

	// The walk stays in one column, so reading the chunk directly suffices and
	// the world's per-block lock is never taken. The box reaches the floor
	// block and the one above, as SpawnBoxClear's does for a player.
	top := w.dim.minY + w.dim.height - 1
	clear := func(y int32) bool {
		if bodiesClear != nil && !bodiesClear(SpawnPos{X: spawn.X, Y: y, Z: spawn.Z}) {
			return false
		}
		for y2 := y; y2 <= y+1; y2++ {
			if y2 > top {
				continue
			}
			state := chunk.BlockAt(spawn.X&15, y2, spawn.Z&15)
			if blocklogic.FluidOf(state).Pure {
				return false
			}
			if !registry.BlockOf(state).NoCollision() {
				return false
			}
		}
		return true
	}

	y := spawn.Y
	for !clear(y) && y < top {
		y++
	}
	y--
	for clear(y) && y > w.dim.minY {
		y--
	}
	y++
	return SpawnPos{X: spawn.X, Y: y, Z: spawn.Z}
}

// AdjustedSpawn runs PlayerSpawnFinder's search: the respawn_radius radius
// clamped to the square it can draw, up to vanilla's 1024 candidates, walked in
// a coprime stride from a random start, falling back to fixing up the
// suggestion itself. A candidate must stand in RespawnColumn and pass boxClear
// (the ground half is SpawnBoxClear; the caller adds its bodies); the fallback
// passes bodiesClear on to FixupSpawnHeight.
//
// The radius arithmetic runs wide and is folded back to the coordinates' width
// at the end, as Java's int arithmetic wraps an outsized radius's candidates
// over the world.
func (w *World) AdjustedSpawn(suggestion SpawnPos, boxClear, bodiesClear func(SpawnPos) bool) SpawnPos {
	radius := max(int64(w.IntRule(RuleRespawnRadius)), 0)
	if w.limit != nil {
		// vanilla: PlayerSpawnFinder.findSpawn
		dist := int64(math.Floor(w.limit.DistanceTo(float64(suggestion.X), float64(suggestion.Z))))
		radius = min(radius, dist)
		if dist <= 1 {
			radius = 1
		}
	}
	side := 2*radius + 1
	count := min(side*side, 1024)
	coprime := int(count) - 1
	if count > 16 {
		coprime = 17
	}
	offset := w.rollN(int(count))
	for i := 0; i < int(count); i++ {
		value := (offset + coprime*i) % int(count)
		deltaX := value % int(side)
		deltaZ := value / int(side)
		candidate := SpawnPos{
			X: int32(int64(suggestion.X) + int64(deltaX) - radius),
			Z: int32(int64(suggestion.Z) + int64(deltaZ) - radius),
		}
		if place, ok := w.RespawnColumn(candidate.X, candidate.Z); ok && boxClear(place) {
			return place
		}
	}
	return w.FixupSpawnHeight(suggestion, bodiesClear)
}

// FindSurfaceSpawn chooses a new dimension's first spawn without a climate
// search (the end, the nether, plugin shapes): the lowest block a body can
// stand on with two free blocks above, in the chunks around the origin. The
// scan runs bottom-up so a roof or sky layer is never taken for the floor.
// vanilla: MinecraftServer.setInitialSpawn
func (w *World) FindSurfaceSpawn() SpawnPos {
	return findSpawnNear(0, 0, func(cx, cz int32) (SpawnPos, bool) {
		pos := ChunkPos{X: cx, Z: cz}
		c, _ := w.Acquire(pos)
		defer w.Release(pos)
		top := w.dim.minY + w.dim.height - 3
		for lx := range int32(16) {
			for lz := range int32(16) {
				for y := w.dim.minY; y < top; y++ {
					if faceFullUp(c.BlockAt(lx, y, lz)) && c.BlockAt(lx, y+1, lz) == airState && c.BlockAt(lx, y+2, lz) == airState {
						return SpawnPos{X: cx*16 + lx, Y: y + 1, Z: cz*16 + lz}, true
					}
				}
			}
		}
		return SpawnPos{}, false
	})
}
