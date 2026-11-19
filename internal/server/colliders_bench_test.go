package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

func BenchmarkSolidCollisionSnapshot(b *testing.B) {
	for _, n := range []int{100, 1000, 2000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			herd := make(entity.Mobs, n)
			for i := range herd {
				herd[i] = entity.NewMob(entity.TypeShulker, int32(i+1), float64(i%50)*4+0.5, 64, float64(i/50)*4+0.5, int64(i))
			}
			var snapshot mobCollisionWorld
			b.ReportAllocs()
			for b.Loop() {
				w := snapshot.reset(nil, nil, herd).(*mobCollisionWorld)
				for _, m := range herd {
					boxes := w.ForMob(m).(entity.BodyWorld).BodyBoxes(entity.NewAABB(m.BlastBox()).Inflate(0.1))
					if len(boxes) != 0 {
						b.Fatal("separated shulker meets another body")
					}
				}
			}
		})
	}
}
