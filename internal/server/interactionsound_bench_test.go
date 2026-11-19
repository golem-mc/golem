package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// BenchmarkInteractionSound selects and encodes fixed interaction sounds.
func BenchmarkInteractionSound(b *testing.B) {
	b.Run("CopperChest", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sound, _ := copperChestHingeSounds(registry.BlockWeatheredCopperChest)
			soundEffectIDAt(sound, protocol.SoundCategoryBlock, 1.5, 64.5, -2.5, 0.5, 1)
		}
	})
	b.Run("Riptide", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			soundEffectIDAt(registry.SoundItemTridentRiptide2, protocol.SoundCategoryPlayer, 1.5, 64.5, -2.5, 1, 1)
		}
	})
}
