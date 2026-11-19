package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkMapHolderPass rebuilds every viewer's holder list for one shared map.
func BenchmarkMapHolderPass(b *testing.B) {
	for _, viewers := range []int{10, 50, 100} {
		b.Run(fmt.Sprint(viewers), func(b *testing.B) {
			s, _ := newTrackerBenchServer(b)
			clear(s.clients)
			id, data := s.maps.Create("minecraft:overworld", 0, 0, 2)
			data.Locked = true
			stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
			stack.SetMapID(id)
			for i := range viewers {
				p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
				p.SetPosition(player.Position{X: float64(i), Y: 64})
				p.Inventory(func(inv *player.Inventory) { inv.Slots[9] = stack })
				c := &client{srv: s, player: p}
				c.joinSeq.Store(uint64(i + 1))
				c.dim.Store(s.allDims()[0])
				s.assignPlayerSlot(c)
				s.clients[p.UUID] = c
			}
			clients := s.playerClients()
			ids := []int32{id}
			for _, c := range clients {
				c.otherHolders(ids)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, c := range clients {
					// Ordinary player ticks also access the inventory before reading maps.
					c.player.Inventory(func(*player.Inventory) {})
					c.otherHolders(ids)
				}
			}
		})
	}
}

// BenchmarkTickMapsHeld sends marker updates for eight decorated, locked inventory maps
// to each viewer, without terrain redraws or a network connection.
func BenchmarkTickMapsHeld(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range 8 {
		id, data := s.maps.Create("minecraft:overworld", 0, 0, 2)
		data.Locked = true
		stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
		stack.SetMapID(id)
		stack.MapDecorations = map[string]protocol.MapDecoration{
			"+": {Type: "minecraft:monument", X: 0, Z: 0, Rotation: 180},
		}
		for _, c := range s.clients {
			c.player.Inventory(func(inv *player.Inventory) { inv.Slots[9+i] = stack })
		}
	}
	// Import targets before measuring the steady-state holder path.
	for _, c := range s.clients {
		c.tickMaps()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, c := range s.clients {
			c.tickMaps()
		}
	}
}
