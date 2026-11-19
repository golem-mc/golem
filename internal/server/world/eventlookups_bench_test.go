package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkWorldEventLookups(b *testing.B) {
	b.Run("template-sides", func(b *testing.B) {
		state := registry.BlockOakFence.DefaultState()
		for b.Loop() {
			_ = RotateState(state, RotateClockwise90)
		}
	})
	b.Run("height-range", func(b *testing.B) {
		mod := compileMod(parseJSON(`{"type":"height_range","height":{"type":"uniform","min_inclusive":{"above_bottom":4},"max_inclusive":{"below_top":8}}}`), 0, true)
		r := &decorRegion{minY: -64, height: 384, rng: levelgen.NewWorldgenRandom(1)}
		for b.Loop() {
			r.walk([]placementMod{mod}, [3]int32{8, 0, 8}, func(_ *decorRegion, _ [3]int32) {})
		}
	})
	b.Run("map-pixels", func(b *testing.B) {
		w, err := NewWorld(b.TempDir(), NewFlatGenerator())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = w.Close() })
		w.Acquire(ChunkPos{})
		w.SetBlock(8, GroundLevel, 8, registry.BlockWater.DefaultState())
		data := &MapData{Dimension: w.Name(), Scale: 1}
		for b.Loop() {
			renderMapFace(w, data, 8, 8, func(int) bool { return true }, func(int, int, byte) bool { return false })
		}
	})
}
