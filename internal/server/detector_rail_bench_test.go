package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkDetectorRailRefresh measures 20 physics ticks over 16 powered
// detectors and their comparators among 128 carts. Contents alternate each
// cycle so scheduled rechecks must read containers and refresh comparator output.
func BenchmarkDetectorRailRefresh(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	p := world.NewPhysics(w)
	p.SetRailCartSource(railCarts{s: s, dim: s.dimensionOfWorld(w)})
	const y = int32(world.GroundLevel + 1)
	for i := range 112 {
		kind := "minecraft:minecart"
		if i%2 == 0 {
			kind = "minecraft:chest_minecart"
		}
		s.spawnMinecart(w, kind, -8.5, float64(y), -8.5)
	}
	var carts []*minecartEntity
	for x := int32(0); x < 16; x += 4 {
		for z := int32(0); z < 16; z += 4 {
			cart := s.spawnMinecart(w, "minecraft:chest_minecart", float64(x)+0.5, float64(y)+0.0625, float64(z)+0.5)
			carts = append(carts, cart)
			for dx := range int32(3) {
				w.SetBlock(x+dx, y-1, z, registry.BlockStone.DefaultState())
			}
			w.SetBlock(x+2, y, z, registry.BlockRedstoneWire.DefaultState())
			comparator := registry.MustState("minecraft:comparator", map[string]string{"facing": "west"})
			w.SetBlock(x+1, y, z, comparator)
			p.NotifyPlaced(x+1, y, z, registry.BlockAir.DefaultState())
			rail := registry.MustState("minecraft:detector_rail", map[string]string{"shape": "east_west"})
			w.SetBlock(x, y, z, rail)
			p.NotifyPlaced(x, y, z, registry.BlockAir.DefaultState())
		}
	}
	var clock int64
	// End each measured cycle after the comparator delay, before changing cargo.
	for range 42 {
		clock++
		p.Tick(clock)
	}
	b.ReportAllocs()
	for b.Loop() {
		s.minecartsMu.Lock()
		for _, cart := range carts {
			count := int32(64)
			if cart.cargo.slots[0].Count != 0 {
				count = 0
			}
			for i := range cart.cargo.slots {
				cart.cargo.slots[i] = protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: count}
			}
		}
		s.minecartsMu.Unlock()
		for range 20 {
			clock++
			p.Tick(clock)
		}
	}
}
