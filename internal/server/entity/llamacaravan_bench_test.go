package entity

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// BenchmarkLlamaCaravanSearch runs every llama's caravan search in herds of
// 2,000 and 8,000 at the llamas-2000 pen's density (2,000 in 60x60), so the
// time per search should not grow with the herd. No llama is leashed, so no
// search joins and each iteration repeats the same work.
func BenchmarkLlamaCaravanSearch(b *testing.B) {
	for _, herd := range []int{2000, 8000} {
		b.Run(fmt.Sprintf("herd=%d", herd), func(b *testing.B) {
			side := 60 * math.Sqrt(float64(herd)/2000)
			rng := rand.New(rand.NewSource(27))
			mobs := make([]*Mob, herd)
			for i := range mobs {
				mobs[i] = NewMob(TypeLlama, int32(i+1), (rng.Float64()-0.5)*side, 64, (rng.Float64()-0.5)*side, int64(i))
			}
			grid := takePushGrid(herd, nil)
			defer grid.release()
			grid.build(mobs)
			view := &tickView{w: newTestWorld(), others: mobs}
			goals := make([]caravanGoal, herd)
			for i, m := range mobs {
				goals[i] = caravanGoal{m: m, speed: llamaCaravanSpeed}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for i, m := range mobs {
					m.push = grid
					if goals[i].CanStart(view) {
						b.Fatal("a llama joined a caravan with no leashed lead")
					}
					m.push = nil
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*herd), "ns/search")
		})
	}
}
