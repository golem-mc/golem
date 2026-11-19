package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// The server half of the spawn search, used for a first join without a
// saved position (vanilla: PrepareSpawnTask) and a respawn without a spawn
// point (vanilla: TeleportTransition's missing-respawn-block fallback).

// adjustSpawnLocation searches the respawn_radius square for a spot a body
// fits, blocking until done. In adventure (the server's game mode, not the
// player's, as vanilla reads it) only the height is fixed up.
// vanilla: ServerPlayer.adjustSpawnLocation
func (s *Server) adjustSpawnLocation(w *world.World, suggestion world.SpawnPos) world.SpawnPos {
	bodiesClear := s.spawnBodiesClear(w)
	if s.defaultGameMode.Load() == protocol.GameModeAdventure {
		return w.FixupSpawnHeight(suggestion, bodiesClear)
	}
	return w.AdjustedSpawn(suggestion, func(sp world.SpawnPos) bool {
		return w.SpawnBoxClear(sp, entity.PlayerWidth, entity.PlayerHeight) && bodiesClear(sp)
	}, bodiesClear)
}

// spawnBodiesClear is the entity half of the candidate check: the bodies
// noCollision(null, ...) counts, which are boats, live shulkers and still
// adult happy ghasts. Players and other mobs never block. Touching counts,
// through vanilla's 1e-7 inflate. The hulls are read once per search.
// vanilla: PlayerSpawnFinder.noCollisionNoLiquid, EntityGetter.getEntityCollisions
func (s *Server) spawnBodiesClear(w *world.World) func(world.SpawnPos) bool {
	hulls := s.hullsByWorld()[w]
	dim := s.dimensionOfWorld(w)
	return func(sp world.SpawnPos) bool {
		const eps = 1e-7
		x, y, z := float64(sp.X)+0.5, float64(sp.Y), float64(sp.Z)+0.5
		box := entity.NewAABB(x-entity.PlayerWidth/2-eps, y-eps, z-entity.PlayerWidth/2-eps,
			x+entity.PlayerWidth/2+eps, y+entity.PlayerHeight+eps, z+entity.PlayerWidth/2+eps)
		if slices.ContainsFunc(hulls, box.Crosses) {
			return false
		}
		s.mobsMu.Lock()
		defer s.mobsMu.Unlock()
		for _, m := range dim.mobs.herd {
			if m.CollidesWithAny() && box.Crosses(entity.NewAABB(m.BlastBox())) {
				return false
			}
		}
		return true
	}
}
