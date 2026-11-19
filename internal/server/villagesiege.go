package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// tickVillageSiege runs the overworld's custom siege spawner.
// vanilla: ServerChunkCache.tickChunks, VillageSiege.tick
func (s *Server) tickVillageSiege(clients []*client) {
	if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) {
		return
	}
	for _, dim := range s.allDims() {
		if dim.siege == nil {
			continue
		}
		players := spawnPlayersOf(clients, dim.w)
		var spawns []entity.SpawnRequest
		enemies := dim.w.SpawnEnemies() && s.world.Difficulty() != rules.DifficultyPeaceful
		// raidsMu guards the village index and precedes mobsMu.
		s.raidsMu.Lock()
		dim.siege.Tick(siegeLevel{dim.w, dim}, s.spawnRng, enemies, players, func(r entity.SpawnRequest) { spawns = append(spawns, r) })
		s.raidsMu.Unlock()
		for _, r := range spawns {
			s.spawnMobArriving(dim, r.Type, r.X, r.Y, r.Z, 0, nil, false, "", true, func(ent *mobEntity) { ent.mob.Yaw = r.Yaw })
		}
	}
}

type siegeLevel struct {
	*world.World
	dim *dimension
}

func (l siegeLevel) IsVillage(x, y, z int32) bool { return l.dim.isVillage(int(x), int(y), int(z)) }
