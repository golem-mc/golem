package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

type benchLodestoneWorld struct{}

func (benchLodestoneWorld) BlockAt(x, y, z int32) uint16  { return 0 }
func (benchLodestoneWorld) ChunkLoadedAt(x, z int32) bool { return true }

var clearLostLodestoneBench protocol.ItemStack

// BenchmarkClearLostLodestone runs the tick guard over component-bearing
// non-compasses and tracked compasses whose lodestone is gone.
func BenchmarkClearLostLodestone(b *testing.B) {
	tracker := protocol.LodestoneTracker{
		Target:  &protocol.LodestoneTarget{Dimension: "minecraft:overworld", X: 2, Y: 64, Z: 1},
		Tracked: true,
	}
	var stacks []protocol.ItemStack
	for _, item := range []registry.Item{registry.ItemStick, registry.ItemCompass, registry.ItemStone, registry.ItemCompass} {
		s := protocol.ItemStack{ItemID: int32(item), Count: 1}
		s.SetLodestoneTracker(tracker)
		stacks = append(stacks, s)
	}
	var w benchLodestoneWorld
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range stacks {
			clearLostLodestoneBench, _ = ClearLostLodestone(w, s, "minecraft:overworld", -64, 320)
		}
	}
}
