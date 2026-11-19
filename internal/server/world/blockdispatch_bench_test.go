package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkResolveBlockCollision(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockStone, registry.BlockOakDoor, registry.BlockOakStairs, registry.BlockChest} {
		b.Run(block.Name(), func(b *testing.B) {
			state := block.DefaultState()
			b.ReportAllocs()
			for range b.N {
				boxes, _ := resolveCollisionBoxes(state)
				if len(boxes) == 0 {
					b.Fatal("missing collision")
				}
			}
		})
	}
}
