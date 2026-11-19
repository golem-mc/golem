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
