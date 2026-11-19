package server

import (
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// entityGate says which chunks entity-tick this tick: those within a player's
// simulation distance, forced chunks and chunks a plugin ticket entity-ticks.
// A chunk that is only loaded holds its non-player entities still.
// vanilla: ServerLevel.tick (entityTickingChunks check), DistanceManager.inEntityTickingRange
type entityGate struct {
	// centres is each world's players' chunks (spectators that skip chunk
	// generation hold no simulation ticket).
	centres map[*world.World][]world.ChunkPos
	reach   int32
}

// newEntityGate reads the players' places, so it is built before any kind
// lock is taken.
func (s *Server) newEntityGate() entityGate {
	dims := s.allDims()
	g := entityGate{centres: make(map[*world.World][]world.ChunkPos, len(dims)), reach: s.simulationDistance.Load()}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.player == nil || !c.simulates() {
			continue
		}
		w := c.theWorld()
		g.centres[w] = append(g.centres[w], playerChunk(c.player.Position()))
	}
	return g
}

// playerChunk is the chunk a player at pos stands in.
func playerChunk(pos player.Position) world.ChunkPos {
	return world.ChunkPos{X: world.ChunkOf(pos.X), Z: world.ChunkOf(pos.Z)}
}

// ticks reports whether an entity at (x, z) in w is stepped.
func (g entityGate) ticks(w *world.World, x, z float64) bool {
	return entityTicksIn(w, g.centres[w], g.reach, world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)})
}

// entityTicksIn reports whether entities in chunk pos of w are stepped, given
// the chunks the world's players stand in.
func entityTicksIn(w *world.World, centres []world.ChunkPos, reach int32, pos world.ChunkPos) bool {
	if chunkRandomTicks(centres, reach, pos) {
		return true
	}
	// A world with no ticket held skips the ticket mutex.
	return w.ChunkForced(pos) || (w.AnyEntityTicketed() && w.ChunkTicketEntityTicking(pos))
}

// blockFilter is the chunk test for w's scheduled ticks and block entities.
// The forced set is read when the filter is built: the filter runs inside
// world walks that hold w.mu, which ChunkForced would take again.
func (g entityGate) blockFilter(w *world.World) func(world.ChunkPos) bool {
	var forced map[world.ChunkPos]struct{}
	if list := w.ForcedChunks(); len(list) > 0 {
		forced = make(map[world.ChunkPos]struct{}, len(list))
		for _, c := range list {
			forced[c] = struct{}{}
		}
	}
	centres := g.centres[w]
	return func(pos world.ChunkPos) bool { return blockTicksIn(w, forced, centres, g.reach, pos) }
}

// blockTicksIn reports whether scheduled ticks and block entities in chunk
// pos of w run: the entity ring plus one (block-ticking level 32), and the
// neighbours of a forced or ticketed chunk.
// vanilla: DistanceManager.inBlockTickingRange, ChunkLevel.isBlockTicking
func blockTicksIn(w *world.World, forced map[world.ChunkPos]struct{}, centres []world.ChunkPos, reach int32, pos world.ChunkPos) bool {
	if chunkRandomTicks(centres, reach+1, pos) {
		return true
	}
	// An entity-ticking chunk (a ticket's inner ring, a test hold) block-ticks too.
	if w.AnyEntityTicketed() && w.ChunkTicketEntityTicking(pos) {
		return true
	}
	ticketed := w.AnyChunkTicketed()
	if len(forced) == 0 && !ticketed {
		return false
	}
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			n := world.ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
			if _, ok := forced[n]; ok || ticketed && w.ChunkTicketed(n) {
				return true
			}
		}
	}
	return false
}

// blockChunk is the chunk holding block column (x, z).
func blockChunk(x, z int32) world.ChunkPos { return world.ChunkPos{X: x >> 4, Z: z >> 4} }

// stepPortal holds cooldowns, crossings and gateway touches with entity simulation.
// A dormant body returns air so its caller cannot queue a gateway touch.
// vanilla: ServerLevel.tickNonPassenger, Entity.handlePortal
func (g entityGate) stepPortal(w *world.World, x, y, z float64, portal *rules.EntityPortal, cooldown int) (uint16, rules.PortalKind, bool) {
	if !g.ticks(w, x, z) {
		return 0, 0, false
	}
	state := w.BlockAt(rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z))
	kind, due := portal.Step(state, rules.PortalTransitionTime, cooldown)
	return state, kind, due
}
