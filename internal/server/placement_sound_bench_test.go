package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"testing"
)

func BenchmarkBlockPlacementSound(b *testing.B) {
	for b.Loop() {
		sound := rules.BlockPlaceSound(registry.BlockOakFence)
		volume, pitch := rules.BlockPlaceSoundLevels(registry.BlockOakFence)
		packet := soundEffectIDAt(sound, protocol.SoundCategoryBlock, 4.5, 64.5, 0.5, volume, pitch)
		if len(packet) == 0 {
			b.Fatal("empty packet")
		}
	}
}
