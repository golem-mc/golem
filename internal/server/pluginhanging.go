package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// hangingRemover is what takes a frame or painting down: the zero value is
// physics (the support is gone).
type hangingRemover struct {
	entity plugin.Entity
	cause  plugin.HangingBreakCause
}

// removedByEntity is a break by the entity with this identity; the zero UUID
// is an entity nobody can name (a shooter that left).
func removedByEntity(id uuid.UUID) hangingRemover {
	if id == uuid.Nil {
		return hangingRemover{cause: plugin.HangingBreakEntity}
	}
	return hangingRemover{entity: pluginEntity{id: id}, cause: plugin.HangingBreakEntity}
}

// removedByBlast is a break by a blast; its remover is the exploder when it
// is also the cause, else the cause (the player who lit the TNT).
func removedByBlast(b blast) hangingRemover {
	r := hangingRemover{cause: plugin.HangingBreakExplosion}
	if b.causeUUID != uuid.Nil && (b.causeID == 0 || b.causeID != b.directID) {
		r.entity = pluginEntity{id: b.causeUUID}
	} else if b.directUUID != uuid.Nil {
		r.entity = (&removedBody{id: b.directUUID, typ: blastEntityType(b)}).handle()
	}
	return r
}

// askHangingBreak puts a frame's or painting's break to HangingBreakEvent
// listeners and reports whether it may break. The caller holds no lock.
// vanilla: BlockAttachedEntity.hurtServer, BlockAttachedEntity.tick
func (s *Server) askHangingBreak(id uuid.UUID, by hangingRemover) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.HangingBreak) {
		return true
	}
	e := &plugin.HangingBreakEvent{Entity: pendingHanging{id: id}, Remover: by.entity, Cause: by.cause}
	s.firePluginEvent(e)
	return !e.Cancelled()
}

// hangingBreakListened reports whether a break asks anyone.
func (s *Server) hangingBreakListened() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.HangingBreak)
}

// removeFrameAsked unlists a frame that breaks, asking the listeners with the
// frame lock released, and reports whether it was removed. The caller holds
// the frame lock; it is released on return.
func (s *Server) removeFrameAsked(f *frameEntity, by hangingRemover) bool {
	if !s.hangingBreakListened() {
		// Already gone when a poll took it out of the lock to ask.
		listed := s.frameByIDLocked(f.w, f.id) == f
		if listed {
			s.removeFrameLocked(f)
		}
		s.framesMu.Unlock()
		return listed
	}
	s.framesMu.Unlock()
	allowed := s.askHangingBreak(f.uuid, by)
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	if !allowed || s.frameByIDLocked(f.w, f.id) != f {
		return false
	}
	s.removeFrameLocked(f)
	return true
}

// removePaintingAsked is removeFrameAsked for a painting.
func (s *Server) removePaintingAsked(p *paintingEntity, by hangingRemover) bool {
	if !s.hangingBreakListened() {
		listed := s.paintingByIDLocked(p.w, p.id) == p
		if listed {
			s.removePaintingLocked(p)
		}
		s.paintingsMu.Unlock()
		return listed
	}
	s.paintingsMu.Unlock()
	allowed := s.askHangingBreak(p.uuid, by)
	s.paintingsMu.Lock()
	defer s.paintingsMu.Unlock()
	if !allowed || s.paintingByIDLocked(p.w, p.id) != p {
		return false
	}
	s.removePaintingLocked(p)
	return true
}
