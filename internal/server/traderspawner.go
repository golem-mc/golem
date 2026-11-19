package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// bellBlock is the block of the minecraft:meeting point of interest.
var bellBlock, _ = registry.BlockByName("minecraft:bell")

// tickTraderSpawner runs the overworld's wandering trader spawner. Off with
// spawn_mobs or spawn_wandering_traders.
// vanilla: ServerChunkCache.tickChunks, WanderingTraderSpawner.tick
func (s *Server) tickTraderSpawner(clients []*client) {
	if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) || !s.world.BoolRule(world.RuleSpawnTraders) {
		return
	}
	for _, dim := range s.allDims() {
		if dim.traders == nil || !dim.traders.Due() {
			continue
		}
		players := spawnPlayersOf(clients, dim.w)
		var spawns []entity.TraderSpawn
		// raidsMu guards the village index and is taken before mobsMu.
		s.raidsMu.Lock()
		s.traderData.Update(func(st *world.TraderSpawnState) {
			dim.traders.Check(traderLevel{World: dim.w, s: s, dim: dim}, s.spawnRng, players, st,
				func(t entity.TraderSpawn) { spawns = append(spawns, t) })
		})
		s.raidsMu.Unlock()
		for _, t := range spawns {
			var trader *mobEntity
			s.spawnMobArriving(dim, entity.TypeWanderingTrader, float64(t.X)+0.5, float64(t.Y), float64(t.Z)+0.5,
				0, nil, false, "", true, func(ent *mobEntity) { t.Prepare(ent.mob); trader = ent })
			for _, at := range t.Llamas {
				s.spawnMobArriving(dim, entity.TypeTraderLlama, float64(at.X)+0.5, float64(at.Y), float64(at.Z)+0.5,
					0, nil, false, "", true, func(ent *mobEntity) { ent.tieLeash(trader.mob.ID, leashRef{holder: trader.uuid}) })
			}
		}
	}
}

// traderLevel is a dimension as the wandering trader spawner reads it; the
// caller holds raidsMu.
type traderLevel struct {
	*world.World
	s   *Server
	dim *dimension
}

// MeetingPoint walks the chunks of PoiManager.getInSquare (z outer, x inner,
// radius/16+1 around the cell's), and in a chunk the lowest section first.
// vanilla: PoiManager.find
func (l traderLevel) MeetingPoint(x, y, z int32, r int) (entity.Waypoint, bool) {
	reach := int32(r/16 + 1)
	cx, cz := x>>4, z>>4
	for qz := cz - reach; qz <= cz+reach; qz++ {
		for qx := cx - reach; qx <= cx+reach; qx++ {
			var best entity.Waypoint
			found := false
			for _, p := range l.dim.villages.InChunk(world.ChunkPos{X: qx, Z: qz}, l.s.raidTick()) {
				dx, dy, dz := p.X-int(x), p.Y-int(y), p.Z-int(z)
				if dx*dx+dy*dy+dz*dz > r*r || registry.BlockOf(l.dim.w.BlockAt(int32(p.X), int32(p.Y), int32(p.Z))) != bellBlock {
					continue
				}
				if !found || p.Y>>4 < int(best.Y)>>4 {
					best, found = entity.Waypoint{X: int32(p.X), Y: int32(p.Y), Z: int32(p.Z)}, true
				}
			}
			if found {
				return best, true
			}
		}
	}
	return entity.Waypoint{}, false
}
