package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkBlockPlacementPlan(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	for _, block := range []registry.Block{registry.BlockOakPlanks, registry.BlockOakDoor, registry.BlockChest, registry.BlockRepeater} {
		b.Run(block.Name(), func(b *testing.B) {
			state := block.DefaultState()
			ctx := rules.PlacementContext{Face: protocol.FaceTop}
			power := s.physicsFor(w)
			nextInt := func(int) int { return 0 }
			b.ReportAllocs()
			for range b.N {
				if _, ok := rules.PlanBlockPlacement(w, power, block, state, ctx, 4, 63, 4, nextInt); !ok {
					b.Fatal("placement refused")
				}
			}
		})
	}
}
