package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkUseItemOnLever includes decoding, block use and the resulting packets
// for an actor and seven observers; mailboxes are drained without socket writes.
func BenchmarkUseItemOnLever(b *testing.B) {
	benchUseItemOnLever(b, false)
}

// BenchmarkPunchThenUseLever uses a lever four ticks into a wooden spear punch,
// including the packets for seven observers.
func BenchmarkPunchThenUseLever(b *testing.B) {
	benchUseItemOnLever(b, true)
}

func benchUseItemOnLever(b *testing.B, punch bool) {
	s, w := newCommandBenchServer(b, 0)
	var clients []*client
	for range 8 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5})
		c := &client{srv: s, player: p, sentViewWorld: w, sentView: newChunkSet(world.ChunkPos{}, 2)}
		c.sentView.add(world.ChunkPos{})
		c.dim.Store(s.allDims()[0])
		c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		clients = append(clients, c)
	}
	// Synthetic clients have no sockets to close during server shutdown.
	b.Cleanup(func() {
		s.relightWG.Wait()
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
	})
	if punch {
		clients[0].player.Inventory(func(inv *player.Inventory) {
			inv.Slots[player.HotbarSlot] = protocol.ItemStack{ItemID: int32(registry.ItemWoodenSpear), Count: 1}
		})
	}
	s.deferWakes.Store(true)
	y := int32(world.GroundLevel)
	w.SetBlock(4, y, 4, registry.BlockLever.DefaultState())
	var packet protocol.Buffer
	packet.PutVarInt(protocol.HandMain)
	packet.PutPosition(4, y, 4)
	packet.PutVarInt(protocol.FaceTop)
	packet.PutF32(0.5)
	packet.PutF32(0.5)
	packet.PutF32(0.5)
	packet.PutBool(false)
	packet.PutBool(false)
	packet.PutVarInt(1)
	payload := packet.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if punch {
			clients[0].swing.active = false
			if err := clients[0].handleSwingArm(nil); err != nil {
				b.Fatal(err)
			}
			for range 4 {
				clients[0].swing.tick()
			}
		}
		if err := clients[0].handleUseItemOn(protocol.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
		s.flushBlockChanges(w)
		for _, c := range clients {
			c.out.mu.Lock()
			clear(c.out.queue)
			c.out.queue = c.out.queue[:0]
			c.out.queued = 0
			c.out.mu.Unlock()
		}
	}
}

// BenchmarkUseItemOnPlainBlock runs the block-use chain for a held block on a
// block no entry of the chain handles.
func BenchmarkUseItemOnPlainBlock(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5})
	c := &client{srv: s, player: p, sentViewWorld: w, sentView: newChunkSet(world.ChunkPos{}, 2)}
	c.sentView.add(world.ChunkPos{})
	c.dim.Store(s.allDims()[0])
	c.out = &mailbox{c: c, wake: make(chan struct{}, 1)}
	s.assignPlayerSlot(c)
	s.clients[p.UUID] = c
	b.Cleanup(func() {
		s.relightWG.Wait()
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
	})
	p.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.HotbarSlot] = protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 64}
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if c.useItemOnBlock(protocol.HandMain, 4, world.GroundLevel-1, 4, rules.PlacementContext{}) {
			b.Fatal("plain block was used")
		}
	}
}
