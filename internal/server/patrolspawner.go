package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// tickPatrolSpawner runs the overworld's custom enemy spawner.
// vanilla: ServerChunkCache.tickChunks, PatrolSpawner.tick
func (s *Server) tickPatrolSpawner(clients []*client) {
	if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) ||
		!s.world.BoolRule(world.RuleSpawnPatrols) {
		return
	}
	for _, dim := range s.allDims() {
		if dim.patrols == nil || !dim.w.SpawnEnemies() || !dim.patrols.Due(s.spawnRng) {
			continue
		}
		players := spawnPlayersOf(clients, dim.w)
		var spawns []entity.PatrolSpawn
		// raidsMu guards the village index and precedes mobsMu.
		s.raidsMu.Lock()
		dim.patrols.Check(patrolLevel{catLevel{World: dim.w, s: s, dim: dim}}, s.spawnRng, players,
			func(p entity.PatrolSpawn) { spawns = append(spawns, p) })
		s.raidsMu.Unlock()
		for _, p := range spawns {
			s.spawnMobArriving(dim, entity.TypePillager, float64(p.At.X), float64(p.At.Y), float64(p.At.Z),
				0, nil, false, "", true, func(ent *mobEntity) {
					ent.mob.DropSpawnLead()
					p.Prepare(ent.mob, ominousBanner())
				})
		}
	}
}

type patrolLevel struct{ catLevel }

func (l patrolLevel) EffectiveDifficulty(x, z int32) float32 {
	return l.s.effectiveDifficultyAt(l.World, float64(x), float64(z))
}

func (l patrolLevel) OverworldClockTicks() int64 {
	_, total, _ := l.ClockTime("minecraft:overworld")
	return total
}
