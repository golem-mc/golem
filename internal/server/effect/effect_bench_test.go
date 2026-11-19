package effect

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkEffectSetTick measures the snapshots and countdown of a body's tick.
func BenchmarkEffectSetTick(b *testing.B) {
	for _, count := range []int{0, 3, len(registry.MobEffects)} {
		b.Run(fmt.Sprintf("effects-%d", count), func(b *testing.B) {
			var s Set
			for id := range count {
				s.Add(New(int32(id), Infinite, 0, false, true, true))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.All()
				s.Tick(1, func(*Instance) bool { return true })
			}
		})
	}
}
