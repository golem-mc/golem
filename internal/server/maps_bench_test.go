package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func BenchmarkTickMapsDistantFrames(b *testing.B) {
	for _, count := range []int{0, 1_000, 8_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			id, data := s.maps.Create(world.DimensionName, 0, 0, 2)
			data.Locked = true
			stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
			stack.SetMapID(id)
			for i := range count {
				f := newFrame(s.nextEntity(), uuid.New(), w, int32(4096+i), world.GroundLevel+1, 0, protocol.FaceSouth, false)
				f.setItem(stack)
				s.registerFrame(f)
			}
			for _, c := range s.clients {
				c.player.Inventory(func(inv *player.Inventory) { inv.Slots[9] = stack })
				c.tickMaps()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, c := range s.clients {
					c.tickMaps()
				}
			}
		})
	}
}

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
				s.mapHolders.built, s.mapHolders.clients = false, s.clientsLocked()
				for _, c := range clients {
					// Ordinary player ticks also access the inventory before reading maps.
					c.player.Inventory(func(*player.Inventory) {})
					c.otherHolders(ids)
				}
			}
		})
	}
}

// BenchmarkTickMapsOwnMap runs five ticks (one marker tick) of every player's
// tickMaps, each player holding its own locked map.
func BenchmarkTickMapsOwnMap(b *testing.B) {
	for _, players := range []int{10, 50, 100} {
		b.Run(fmt.Sprint(players), func(b *testing.B) {
			s, _ := newIdlePlayersBenchServer(b, players)
			clients := s.playerClients()
			for _, c := range clients {
				id, data := s.maps.Create(world.DimensionName, 0, 0, 2)
				data.Locked = true
				stack := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1}
				stack.SetMapID(id)
				c.player.Inventory(func(inv *player.Inventory) { inv.Slots[9] = stack })
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for range mapUpdateInterval {
					s.mapHolders.built, s.mapHolders.clients = false, s.clientsLocked()
					for _, c := range clients {
						c.player.Inventory(func(*player.Inventory) {})
						c.tickMaps()
					}
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
