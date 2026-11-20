package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// portalNote is a portal contact or arrival an entity pass saw, held until
// the tick's passes are done so listeners run with no entity lock held.
type portalNote struct {
	who  command.Entity // nil resolves id
	id   uuid.UUID
	exit bool

	// contact: the block touched
	w       *world.World
	x, y, z int32
	kind    plugin.PortalKind

	// arrival: where it left, and the portal it used
	from  plugin.Location
	cause plugin.TeleportCause
}

// portalNotes is the queue of portalNote, guarded by mu: a player's crossing
// goroutine queues its arrival too.
type portalNotes struct {
	mu    sync.Mutex
	notes []portalNote
}

func (s *Server) portalNoteListening(id int) bool {
	return s.plugins != nil && s.plugins.Has(id)
}

// notePortalTouch queues EntityPortalEnterEvent for a body whose box is in
// the block at (x, y, z) when that is a portal of state. who is the body, or
// nil to find it by id.
// vanilla: BaseFireBlock/NetherPortalBlock/EndPortalBlock/EndGatewayBlock.entityInside
func (s *Server) notePortalTouch(who command.Entity, id uuid.UUID, w *world.World, x, y, z float64, state uint16) {
	if !s.portalNoteListening(pluginruntime.EntityPortalEnter) {
		return
	}
	var kind plugin.PortalKind
	switch {
	case rules.IsPortalState(state):
		kind = plugin.PortalKindNether
	case state == rules.EndPortalState:
		kind = plugin.PortalKindEnd
	case state == rules.EndGatewayState:
		kind = plugin.PortalKindEndGateway
	default:
		return
	}
	s.queuePortalNote(portalNote{who: who, id: id, w: w, x: rules.BlockOf(x), y: rules.BlockOf(y), z: rules.BlockOf(z), kind: kind})
}

// notePortalExit queues EntityPortalExitEvent for a body that has arrived;
// from is read before it left.
func (s *Server) notePortalExit(who command.Entity, id uuid.UUID, from plugin.Location, kind rules.PortalKind) {
	cause := plugin.TeleportNetherPortal
	if kind == rules.PortalEnd {
		cause = plugin.TeleportEndPortal
	}
	s.queuePortalNote(portalNote{who: who, id: id, exit: true, from: from, cause: cause})
}

func (s *Server) queuePortalNote(n portalNote) {
	s.portalNotes.mu.Lock()
	s.portalNotes.notes = append(s.portalNotes.notes, n)
	s.portalNotes.mu.Unlock()
}

// portalExitSource reads what an exit event needs of the body before it
// crosses: its handle's identity and where it stands. It is false when no
// listener wants the exit or the body cannot be found.
func (s *Server) portalExitSource(w *world.World, entityID int32) (command.Entity, plugin.Location, bool) {
	if !s.portalNoteListening(pluginruntime.EntityPortalExit) {
		return nil, plugin.Location{}, false
	}
	list := (worldScope{srv: s}).Entities(command.EntityQuery{Dimension: s.dimensionOfWorld(w).name, Limit: 1,
		Accept: func(e command.Entity) bool { return e.EntityID() == entityID }})
	if len(list) == 0 {
		return nil, plugin.Location{}, false
	}
	return list[0], s.pluginAdapter.entityLocation(list[0]), true
}

// flushPortalNotes fires the queued events, with no lock held.
func (s *Server) flushPortalNotes() {
	s.portalNotes.mu.Lock()
	notes := s.portalNotes.notes
	s.portalNotes.notes = nil
	s.portalNotes.mu.Unlock()
	if len(notes) == 0 || s.plugins == nil {
		return
	}
	h := s.pluginAdapter
	for _, n := range notes {
		var ent plugin.Entity
		who := n.who
		if who == nil {
			if b, ok := (pluginEntity{host: h, id: n.id}).live(); ok {
				who = b
			}
		}
		if who != nil {
			ent = h.pluginEntityOf(who)
		} else {
			ent = pluginPlainEntity{pluginEntity{host: h, id: n.id}}
		}
		if n.exit {
			if who != nil {
				s.plugins.Fire(&plugin.EntityPortalExitEvent{Entity: ent, From: n.from, To: h.entityLocation(who), Cause: n.cause})
			}
			continue
		}
		pw := pluginWorld{srv: s, dim: s.dimensionOfWorld(n.w), host: h}
		s.plugins.Fire(&plugin.EntityPortalEnterEvent{Entity: ent,
			Block: plugin.Block{World: pw, Pos: plugin.BlockPos{X: int(n.x), Y: int(n.y), Z: int(n.z)}}, Kind: n.kind})
	}
}
