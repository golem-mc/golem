package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkVehicleCargoBroadcast measures notifying a second cargo viewer after
// the clicker has already received its correction, through packet enqueueing.
func BenchmarkVehicleCargoBroadcast(b *testing.B) {
	for _, kind := range []string{"cart", "boat"} {
		b.Run(kind, func(b *testing.B) {
			s := &Server{clients: make(map[uuid.UUID]*client)}
			cart := &minecartEntity{cargo: newVehicleCargo(27)}
			boat := &boatEntity{cargo: make([]protocol.ItemStack, 27)}
			view := windowView{cart: cart}
			slots := cart.cargo.slots
			if kind == "boat" {
				view = windowView{boat: boat}
				slots = boat.cargo
			}
			clients := make([]*client, 2)
			for i := range clients {
				c := &client{srv: s, player: &player.Player{}, out: &mailbox{wake: make(chan struct{}, 1)}}
				window := &openWindow{id: 1, cart: view.cart, boat: view.boat}
				window.Sent = make([]protocol.ItemStack, 27)
				c.setOpenWindow(window)
				s.assignPlayerSlot(c)
				s.clients[uuid.New()] = c
				clients[i] = c
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				slots[7] = protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: int32(i%64 + 1)}
				clients[0].openContainerWindow().setSent(7, slots[7])
				s.broadcastEntitySlot(view, 7, clients[0])
				// There is no socket writer: drain the receiver's mailbox each time.
				clients[1].out.queue = clients[1].out.queue[:0]
				clients[1].out.queued = 0
			}
		})
	}
}

// BenchmarkVehicleCargoNoViewers measures syncCartViewers with 100 connected
// clients, no open menus, and a 27-slot cart.
func BenchmarkVehicleCargoNoViewers(b *testing.B) {
	s := &Server{clients: make(map[uuid.UUID]*client, 100)}
	cart := &minecartEntity{cargo: newVehicleCargo(27)}
	for range 100 {
		c := &client{srv: s, player: &player.Player{}, out: &mailbox{wake: make(chan struct{}, 1)}}
		s.assignPlayerSlot(c)
		s.clients[uuid.New()] = c
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.syncCartViewers(cart)
	}
}

func TestSyncVehicleViewersSnapshotAndWindowIdentity(t *testing.T) {
	t.Parallel()
	s := &Server{clients: make(map[uuid.UUID]*client)}
	cart := &minecartEntity{cargo: newVehicleCargo(27)}
	diamond := protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 3}

	// Ten viewers exercise the stack-buffer overflow past 8.
	viewers := make([]*client, 10)
	windows := make([]*openWindow, len(viewers))
	for i := range viewers {
		c := &client{srv: s, player: &player.Player{}, out: &mailbox{wake: make(chan struct{}, 1)}}
		w := &openWindow{id: 1, cart: cart}
		w.Sent = make([]protocol.ItemStack, 27)
		c.setOpenWindow(w)
		s.assignPlayerSlot(c)
		s.clients[uuid.New()] = c
		viewers[i] = c
		windows[i] = w
	}

	// Simulate viewer 0 closing and reopening another menu after matching but
	// before sendContainerChanges runs under the player inventory lock.
	reopened := &openWindow{id: 2}
	reopened.Sent = make([]protocol.ItemStack, 27)
	calls := 0
	s.syncVehicleViewers(func(w *openWindow) bool { return w.cart == cart }, func() []protocol.ItemStack {
		calls++
		viewers[0].setOpenWindow(nil)
		viewers[0].setOpenWindow(reopened)
		snap := make([]protocol.ItemStack, 27)
		snap[4] = diamond
		return snap
	})
	if calls != 1 {
		t.Fatalf("slots callback called %d times, want 1", calls)
	}
	if len(viewers[0].out.queue) != 0 {
		t.Fatalf("reopened viewer received %d packets for old window, want 0", len(viewers[0].out.queue))
	}
	if !reopened.Sent[4].Empty() {
		t.Fatalf("reopened window Sent[4] = %+v, want empty", reopened.Sent[4])
	}
	for i := 1; i < len(viewers); i++ {
		if len(viewers[i].out.queue) != 1 {
			t.Fatalf("viewer %d received %d packets, want 1", i, len(viewers[i].out.queue))
		}
		if !windows[i].Sent[4].Equal(diamond) {
			t.Fatalf("viewer %d Sent[4] = %+v, want %+v", i, windows[i].Sent[4], diamond)
		}
	}
}
