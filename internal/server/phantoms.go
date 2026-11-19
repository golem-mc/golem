package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// tickPhantomSpawners runs each dimension's phantom spawner (only the
// overworld has one), spawning phantoms over players who have not slept.
// Off with spawn_mobs, the enemy flag or spawn_phantoms; it only counts down
// while the dimension has players.
// vanilla: ServerChunkCache.tickChunks, PhantomSpawner.tick
func (s *Server) tickPhantomSpawners(clients []*client) {
	if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) || !s.world.BoolRule(world.RuleSpawnPhantoms) {
		return
	}
	for _, dim := range s.allDims() {
		if dim.phantoms == nil || !dim.w.SpawnEnemies() {
			continue
		}
		var players []entity.PhantomPlayer
		for _, c := range clients {
			if c.theWorld() != dim.w {
				continue
			}
			pos := c.player.Position()
			players = append(players, entity.PhantomPlayer{
				X: pos.X, Y: pos.Y, Z: pos.Z,
				Spectator:     c.player.GameMode() == protocol.GameModeSpectator,
				TimeSinceRest: int(c.statValue(player.Custom(registry.CustomStatTimeSinceRest))),
				Effective:     s.effectiveDifficultyAt(dim.w, pos.X, pos.Z),
			})
		}
		var requests []entity.SpawnRequest
		dim.phantoms.Tick(dim.w, s.spawnRng, players,
			func(r entity.SpawnRequest) { requests = append(requests, r) })
		for _, r := range requests {
			s.spawnMobInDim(dim, r.Type, r.X, r.Y, r.Z, 0, nil)
		}
	}
}
