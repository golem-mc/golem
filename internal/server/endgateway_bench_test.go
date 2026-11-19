package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickEndGateways runs the gateway pass with one gateway per held chunk.
func BenchmarkTickEndGateways(b *testing.B) {
	benchmarkTickEndGateways(b, 1, 0)
}

// BenchmarkTickEndGatewaysPopulated mixes 32 gateways with 224 other carried
// entities per chunk so repeated list searches cannot hide behind singleton lists.
func BenchmarkTickEndGatewaysPopulated(b *testing.B) {
	benchmarkTickEndGateways(b, 32, 224)
}

func benchmarkTickEndGateways(b *testing.B, gateways, others int) {
	s, w := newTrackerBenchServer(b)
	y := int32(world.GroundLevel) + 5
	n := 0
	for cx := int32(-3); cx < 3; cx++ {
		for cz := int32(-3); cz < 3; cz++ {
			for i := range gateways + others {
				x, z := cx*16+int32(i%16), cz*16+int32(i/16)
				if i < gateways {
					w.SetBlock(x, y, z, rules.EndGatewayState)
					s.SetEndGatewayExit(x, y, z, 0, 80, 0, false)
					n++
				} else {
					w.SetBlock(x, y, z, registry.BlockEnchantingTable.DefaultState())
					w.SetBlockEntityTag(x, y, z, "minecraft:enchanting_table", nbt.Compound{})
				}
			}
		}
	}
	if got := len(w.TickingCarriedOfKind("minecraft:end_gateway")); got != n {
		b.Fatalf("%d ticking gateways, want %d", got, n)
	}
	s.tickEndGateways(w, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.ticksRun.Add(1)
		s.tickEndGateways(w, nil)
	}
}

// BenchmarkEndGatewayMove includes the off-tick exit search and move dispatch.
func BenchmarkEndGatewayMove(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	// Keep the benchmark on the move dispatch rather than watcher queues.
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	s.SetEndGatewayExit(3, 90, 3, 20, 120, 20, true)
	var mu sync.Mutex
	var portal rules.EntityPortal
	moved := make(chan player.Position)
	g := gatewayTouch{
		w: w, x: 3, y: 90, z: 3, mu: &mu, portal: &portal,
		move: func(to player.Position) { moved <- to },
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.ticksRun.Add(rules.EndGatewayCooldownTicks)
		s.touchGateway(g)
		if to := <-moved; to.X != 20.5 || to.Y != 120 || to.Z != 20.5 {
			b.Fatalf("landing = %+v", to)
		}
	}
}
