package server

import (
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// addPickupBenchClient registers a survival player standing at x, 64, z.
func addPickupBenchClient(s *Server, dim *dimension, x, z float64) *client {
	p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: x, Y: 64, Z: z})
	c := &client{srv: s, player: p}
	c.dim.Store(dim)
	s.assignPlayerSlot(c)
	s.mu.Lock()
	s.clients[p.UUID] = c
	s.mu.Unlock()
	return c
}

// TestPlayerItemPickupIndexOrder checks the index names the same collector as
// the direct loop for overlapping boxes across a chunk border, a mounted
// player's wider box, an owner-only item and another dimension.
func TestPlayerItemPickupIndexOrder(t *testing.T) {
	s, w := newTrackerBenchServer(t)
	// Only the world's identity matters to the pickup check.
	other := &dimension{w: &world.World{}}
	dim := s.allDims()[0]
	var clients []*client
	// Players straddling the x=16 chunk border, boxes overlapping.
	for i := range 12 {
		clients = append(clients, addPickupBenchClient(s, dim, 13+float64(i)*0.7, 3))
	}
	// One far out whose vehicle box is wide.
	clients = append(clients, addPickupBenchClient(s, dim, 100, 100))
	// Same XYZ as a client, but in another dimension.
	foreign := addPickupBenchClient(s, dim, 16, 3)
	foreign.dim.Store(other)
	clients = append(clients, foreign)

	reach := make([]entity.AABB, len(clients))
	for i, c := range clients {
		pos := c.player.Position()
		reach[i] = entity.PlayerTouchBox(pos.X, pos.Y, pos.Z, 0.6, 1.8, entity.AABB{})
	}
	// The far player rides something reaching back to the border.
	reach[12] = entity.PlayerTouchBox(100, 64, 100, 0.6, 1.8, entity.NewAABB(12, 64, 2, 101, 65, 101))

	ix := &pickupIndex{clients: clients, reach: reach}
	ix.build()
	if !ix.indexed {
		t.Fatal("index not used")
	}
	direct := func(it *itemEntity) *client {
		for i, c := range clients {
			if c.theWorld() == it.w && canPickUp(c) && it.item.AllowsPickup(c.player.UUID) && reach[i].Crosses(entity.NewAABB(it.item.BlastBox())) {
				return c
			}
		}
		return nil
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	owner := clients[5].player.UUID
	var names []int
	for i := range 80 {
		x := 11 + float64(i)*0.2
		it := s.newItemEntity(w, stack, x, 64, 3, 0, 0, 0, 0)
		if i%3 == 0 {
			it.item.Target = &owner
		}
		want := direct(it)
		got := ix.candidate(it)
		if (got == nil) != (want == nil) || got != nil && got.collector != want {
			t.Fatalf("item %d at x=%v: index %v, direct %v", i, x, got, want)
		}
		if want != nil {
			names = append(names, slices.Index(clients, want))
		}
	}
	// Item at the vehicle's far corner is only the rider's.
	far := s.newItemEntity(w, stack, 100, 64, 100, 0, 0, 0, 0)
	if got := ix.candidate(far); got == nil || got.collector != clients[12] {
		t.Fatalf("far item: %v", got)
	}
	// Equal XYZ in a world no client but the foreign one is in.
	elsewhere := s.newItemEntity(other.w, stack, 16, 64, 3, 0, 0, 0, 0)
	if got := ix.candidate(elsewhere); got == nil || got.collector != foreign {
		t.Fatalf("other-dimension item: %v", got)
	}
	// A vehicle reaching past eight cells turns the index off for the pass.
	reach[12] = entity.PlayerTouchBox(100, 64, 100, 0.6, 1.8, entity.NewAABB(12, 64, 2, 301, 65, 101))
	ix.build()
	if ix.indexed {
		t.Fatal("index used for a box spanning more than eight cells")
	}
	if len(names) < 20 || slices.Max(names) == 0 {
		t.Fatalf("test covers too little: %v", names)
	}
}

// BenchmarkDistantPlayerItemPickups times a pickup pass over 2,000 eligible
// drops outside every player's box.
func BenchmarkDistantPlayerItemPickups(b *testing.B) {
	for _, n := range []int{1, 20, 100} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			s.mu.Lock()
			clear(s.clients)
			s.mu.Unlock()
			dim := s.allDims()[0]
			for i := range n {
				addPickupBenchClient(s, dim, float64(4*(i%10)), float64(4*(i/10)))
			}
			stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
			for i := range 2000 {
				s.spawnItem(w, stack, 500+float64(i%64), 64, float64(i/64), 0, 0, 0, 0)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickItemPickups()
			}
			b.StopTimer()
			for _, it := range s.items {
				if it.item.Dead {
					b.Fatal("an item was picked up")
				}
			}
		})
	}
}
