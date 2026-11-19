package server

import (
	"io"
	"net"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkContainerStillOpen(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	y := int32(world.GroundLevel)
	for _, name := range []string{"barrel", "double_chest"} {
		b.Run(name, func(b *testing.B) {
			window := &openWindow{kind: windowBarrel, w: w, x: 4, y: y, z: 4}
			block, kind := registry.BlockBarrel, world.KindBarrel
			state := block.DefaultState()
			if name == "double_chest" {
				window.kind = windowDouble
				window.second = [3]int32{5, y, 4}
				block, kind = registry.BlockChest, world.KindChest
				state = block.DefaultState()
				state, _ = registry.WithProp(state, registry.PropType, "right")
				second, _ := registry.WithProp(state, registry.PropType, "left")
				w.SetBlock(5, y, 4, second)
				w.EnsureBlockEntity(5, y, 4, kind)
			}
			w.SetBlock(4, y, 4, state)
			w.EnsureBlockEntity(4, y, 4, kind)
			if !c.containerStillOpen(window) {
				b.Fatal("window invalid")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !c.containerStillOpen(window) {
					b.Fatal("window closed")
				}
			}
		})
	}
}

func BenchmarkContainerStillOpenMerchant(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	server, remote := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, remote) }()
	b.Cleanup(func() { server.Close(); remote.Close() })
	c := &client{srv: s, player: p, conn: protocol.NewConn(server)}
	c.dim.Store(s.allDims()[0])
	recipe := nbt.Compound{
		"buy":     nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(2)},
		"sell":    nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
		"maxUses": nbt.Int(16),
	}
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
	id := s.SpawnVillagerWithOffers("minecraft:farmer", 1, offers, 2.5, float64(world.GroundLevel), 5.5)
	s.mobsMu.Lock()
	ent := s.mobByEntityIDLocked(w, id)
	s.mobsMu.Unlock()
	if _, err := c.useOnVillager(ent, protocol.HandMain); err != nil {
		b.Fatal(err)
	}
	window := c.container.window
	if window == nil || window.trader == nil {
		b.Fatal("no merchant window open")
	}
	if !c.containerStillOpen(window) {
		b.Fatal("window invalid")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !c.containerStillOpen(window) {
			b.Fatal("window closed")
		}
	}
}
