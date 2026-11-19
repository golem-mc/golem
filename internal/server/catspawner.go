package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// tickCatSpawner runs the overworld's stray cat spawner. Off with spawn_mobs.
// vanilla: ServerChunkCache.tickChunks, CatSpawner.tick
func (s *Server) tickCatSpawner(clients []*client) {
	if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) {
		return
	}
	for _, dim := range s.allDims() {
		if dim.cats == nil {
			continue
		}
		var players []entity.SpawnPlayer
		for _, c := range clients {
			if c.theWorld() != dim.w {
				continue
			}
			pos := c.player.Position()
			d := c.dims(c.player.Appearance().Pose)
			players = append(players, entity.SpawnPlayer{X: pos.X, Y: pos.Y, Z: pos.Z, Width: d.Width, Height: d.Height,
				Spectator: c.player.GameMode() == protocol.GameModeSpectator, Dead: c.player.Vitals().Dead})
		}
		var requests []entity.SpawnRequest
		// raidsMu guards the village index and is taken before mobsMu.
		s.raidsMu.Lock()
		dim.cats.Tick(catLevel{World: dim.w, s: s, dim: dim}, s.spawnRng, players,
			func(r entity.SpawnRequest) { requests = append(requests, r) })
		s.raidsMu.Unlock()
		for _, r := range requests {
			s.spawnMobArriving(dim, r.Type, r.X, r.Y, r.Z, 0, nil, false, "", true, func(ent *mobEntity) { ent.mob.Persistence = r.Persistent })
		}
	}
}

// catLevel is a dimension as the cat spawner reads it; the caller holds
// raidsMu.
type catLevel struct {
	*world.World
	s   *Server
	dim *dimension
}

func (l catLevel) CloseToVillage(x, y, z int32, sections int) bool {
	return l.dim.villages.SectionsToVillage(int(x), int(y), int(z), sections) <= sections
}

func (l catLevel) VillageBeds(x, y, z int32, r int) int {
	beds := 0
	for _, p := range l.dim.villages.OccupiedAround(int(x), int(y), int(z), r) {
		if registry.BlockOf(l.dim.w.BlockAt(int32(p.X), int32(p.Y), int32(p.Z))).InTag(bedsTag) {
			beds++
		}
	}
	return beds
}

func (l catLevel) CatsIn(x, y, z int32, rx, ry float64) int {
	l.s.mobsMu.Lock()
	defer l.s.mobsMu.Unlock()
	n := 0
	for m := range l.dim.mobs.book {
		if m.IsCatNear(x, y, z, rx, ry) {
			n++
		}
	}
	return n
}
