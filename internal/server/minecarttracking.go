package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// tickMinecartTracking advances tracking before physics, including frozen ticks.
// vanilla: ServerChunkCache.tick, ChunkMap.tick, ServerEntity.sendChanges
func (s *Server) tickMinecartTracking() {
	s.lockTick(&s.minecartsMu)
	empty := len(s.minecarts) == 0
	s.unlockTick(&s.minecartsMu)
	if empty {
		return
	}
	watchers := s.trackingWatchers()
	tick := s.trackingTick()
	near := nearbyTracking(watchers)
	defer near.release()
	var out trackSends
	var metas sendList
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		if it.cart.Dead || !it.w.ChunkLoaded(world.ChunkPos{X: world.ChunkOf(it.cart.X), Z: world.ChunkOf(it.cart.Z)}) {
			continue
		}
		body := minecartTracker.body(it)
		// Consume the flag on interval ticks too, so it cannot force the
		// following tick.
		synced := it.takeSync()
		pending := it.cart.TakeBounce() || synced || it.teleported
		mt := body.schedule(pending)
		now := minecartMetaOf(it)
		still, prevTick := body.stillSince(tick)
		for _, i := range body.watchers(near) {
			watcher := watchers[i]
			c := watcher.c
			if c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			was, seen := body.settleOrTold(watcher, still, prevTick, &out)
			if !seen {
				continue
			}
			// Metadata changes are independent of the movement schedule.
			if was.own != now {
				if data := minecartDeltaMetadata(it.cart.ID, &was.own, &now); data != nil {
					metas.add(c, outbound{id: protocol.IDPlayEntityMetadata, data: data})
				}
				was.own = now
			}
			if mt.gate {
				body.moveTold(was, c, mt, &out)
			}
		}
	}
	s.unlockTick(&s.minecartsMu)
	out.send()
	metas.send()
}
