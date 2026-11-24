package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestRemovedPickupCandidateIsSkipped(t *testing.T) {
	check := func(t *testing.T, removeDuringCallback bool) {
		s, pw := newPluginHandleTestWorld(t)
		c := &client{srv: s, player: &player.Player{EntityID: s.nextEntity()}}
		c.dim.Store(pw.dim)
		reach := map[*client]entity.AABB{c: c.pickupBox()}
		stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
		original := s.newItemEntity(pw.dim.w, stack, 0, 0, 0, 0, 0, 0, 0)
		replacement := s.newItemEntity(pw.dim.w, stack, 0, 0, 0, 0, 0, 0, 0)
		replacement.uuid = original.uuid

		s.itemsMu.Lock()
		s.appendItemLocked(original)
		candidate := itemPickupCandidate(original, []*client{c}, reach)
		s.itemsMu.Unlock()
		if candidate == nil {
			t.Fatal("original item is out of pickup reach")
		}

		replace := func() {
			s.itemsMu.Lock()
			// An item crossing dimensions is unfiled while still alive.
			itemTracker.unfile(original)
			s.items = s.items[:0]
			s.appendItemLocked(replacement)
			s.itemsMu.Unlock()
		}
		listener := &pickupTestPlugin{onPickup: func() {
			if removeDuringCallback {
				replace()
			}
		}}
		s.loadPlugins([]plugin.Registration{{Name: "pickup", Plugin: listener}})
		s.enablePlugins()
		s.plugins.BindTick()
		wantCalls := 0
		if removeDuringCallback {
			wantCalls = 1
		} else {
			replace()
		}

		s.giveItemPickups([]*itemPickup{candidate})
		if listener.calls != wantCalls {
			t.Errorf("pickup callbacks = %d, want %d", listener.calls, wantCalls)
		}
		c.player.Inventory(func(inv *player.Inventory) {
			if got := inv.Slots[player.HotbarSlot]; !got.Empty() {
				t.Errorf("collector received %+v from a removed candidate", got)
			}
		})
		if replacement.item.Dead || replacement.item.Stack.Count != 1 {
			t.Error("claim changed the replacement item with the same UUID")
		}
	}
	t.Run("before callback", func(t *testing.T) { check(t, false) })
	t.Run("during callback", func(t *testing.T) { check(t, true) })
}

type pickupTestPlugin struct {
	onPickup func()
	calls    int
}

func (p *pickupTestPlugin) Enable(s plugin.Server) error {
	plugin.Listen(s, func(*plugin.EntityPickupItemEvent) {
		p.calls++
		p.onPickup()
	})
	return nil
}

func (*pickupTestPlugin) Disable() {}
