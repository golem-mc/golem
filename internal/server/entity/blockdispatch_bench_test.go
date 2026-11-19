package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

var landPathfindableBenchResult bool

func BenchmarkLandPathfindable(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockStone, registry.BlockOakDoor, registry.BlockChest, registry.BlockOakSlab} {
		b.Run(block.Name(), func(b *testing.B) {
			state := block.DefaultState()
			b.ReportAllocs()
			for range b.N {
				landPathfindableBenchResult = landPathfindable(state)
			}
		})
	}
}
