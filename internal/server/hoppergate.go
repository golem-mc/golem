package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// moveGate carries one hopper direction's plugin decision into the locked
// move: a probing pass only records the first item the move would transfer
// (writing nothing), the real pass applies the decision.
type moveGate struct {
	probing, seen, decided, cancelled bool
	stack, repl                       protocol.ItemStack
	target                            containerTarget
}

// Check implements inventory.Gate.
func (g *moveGate) Check(one protocol.ItemStack) (protocol.ItemStack, bool) {
	switch {
	case g.probing:
		if !g.decided {
			g.seen, g.stack = true, one
		}
		return one, false
	case g.decided && g.cancelled:
		return one, false
	case g.decided:
		return g.repl, true
	}
	return one, true
}

// hopperGate holds the plugin decisions for one hopper turn. They are asked
// before the turn's locks are taken, because the listeners get inventory
// handles that lock the same containers.
type hopperGate struct {
	push, pull moveGate
	probing    bool
	pickup     map[*itemEntity]bool // decided item entities, true when cancelled
	pending    *itemEntity          // first undecided entity of a probe
}

// vetoed reports whether a listener cancelled a move, which still costs the
// hopper its cooldown.
func (g *hopperGate) vetoed() bool {
	return g.push.cancelled || g.pull.cancelled
}

// hopperListening reports whether a plugin listens to a hopper's moves.
func (s *Server) hopperListening() bool {
	return s.plugins != nil && (s.plugins.Has(pluginruntime.InventoryMoveItem) || s.plugins.Has(pluginruntime.InventoryPickupItem))
}

// askHopper probes one hopper's turn and fires the move and pickup events for
// what it would do, outside the hopper's lock. A cancelled pickup is probed
// again, since the hopper then tries the next item entity.
func (s *Server) askHopper(w *world.World, be *world.BlockEntity, age int64) *hopperGate {
	g := &hopperGate{pickup: map[*itemEntity]bool{}}
	for {
		g.probing, g.pending = true, nil
		g.push.probing, g.pull.probing = true, true
		g.push.seen, g.pull.seen = false, false
		be.UseLive(func(b *world.BlockEntity) {
			if b.Cooldown > 1 || !world.HopperEnabled(w, b.X, b.Y, b.Z) {
				return
			}
			s.hopperStep(w, b, g, &hopperTurn{})
		})
		g.probing, g.push.probing, g.pull.probing = false, false, false
		if !g.push.seen && !g.pull.seen && g.pending == nil {
			return g
		}
		again := s.fireHopperEvents(w, be, g)
		if !again {
			return g
		}
	}
}

// fireHopperEvents asks the listeners about the candidates a probe found and
// reports whether a cancelled pickup calls for another probe.
func (s *Server) fireHopperEvents(w *world.World, be *world.BlockEntity, g *hopperGate) (again bool) {
	fire := func() {
		pw := pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}
		self := containerInventory(pw, be)
		decide := func(m *moveGate, pull bool) {
			if !m.seen {
				return
			}
			m.decided = true
			other := (*world.BlockEntity)(nil)
			if m.target != nil {
				other = m.target.entity()
			}
			if self == nil || other == nil || !s.plugins.Has(pluginruntime.InventoryMoveItem) {
				return
			}
			far := containerInventory(pw, other)
			e := &plugin.InventoryMoveItemEvent{Source: self, Destination: far, Item: pluginStack(m.stack)}
			if pull {
				e.Source, e.Destination = far, self
			}
			s.plugins.Fire(e)
			m.cancelled = e.Cancelled()
			if repl, ok := pluginProtocolStack(e.Item); ok && !repl.Empty() {
				repl.Count = 1
				m.repl = repl
			} else {
				m.repl = m.stack
			}
		}
		decide(&g.push, false)
		decide(&g.pull, true)
		if it := g.pending; it != nil {
			cancelled := false
			if self != nil && s.plugins.Has(pluginruntime.InventoryPickupItem) {
				e := &plugin.InventoryPickupItemEvent{Inventory: self, Item: pluginItemEntity{pluginEntity{host: s.pluginAdapter, id: it.uuid}}}
				s.plugins.Fire(e)
				cancelled = e.Cancelled()
			}
			g.pickup[it] = cancelled
			again = cancelled
		}
	}
	if s.plugins.OnTick() {
		fire()
	} else {
		s.plugins.Call(fire)
	}
	return again
}

// containerInventory is a block container's plugin inventory, nil for a
// block entity that is not one.
func containerInventory(pw pluginWorld, be *world.BlockEntity) plugin.Inventory {
	if c, ok := pluginBlockEntityHandle(pw, be).(plugin.Container); ok {
		return c.Inventory()
	}
	return nil
}
