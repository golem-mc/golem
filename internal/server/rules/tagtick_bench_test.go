package rules

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func BenchmarkTagTickRules(b *testing.B) {
	b.Run("BeeAttracts", func(b *testing.B) {
		states := [...]uint16{registry.BlockDandelion.DefaultState(), registry.BlockStone.DefaultState(), registry.BlockSunflower.DefaultState()}
		b.ReportAllocs()
		for b.Loop() {
			for _, s := range states {
				BeeAttracts(s)
			}
		}
	})
	b.Run("Vibration", func(b *testing.B) {
		states := [...]uint16{registry.BlockWhiteWool.DefaultState(), registry.BlockStone.DefaultState(), registry.BlockRedCarpet.DefaultState()}
		b.ReportAllocs()
		for b.Loop() {
			for _, s := range states {
				VibrationOf(GameEventStep, 0, 64, 0, GameEventContext{Affected: s})
			}
		}
	})
	b.Run("BeaconPyramid", func(b *testing.B) {
		w := newAttachWorld(b)
		for y := int32(60); y < 64; y++ {
			for x := int32(-4); x <= 4; x++ {
				for z := int32(-4); z <= 4; z++ {
					w.SetBlock(x, y, z, registry.BlockIronBlock.DefaultState())
				}
			}
		}
		b.ReportAllocs()
		for b.Loop() {
			BeaconPyramidLevel(w, 0, 64, 0, 0)
		}
	})
}
