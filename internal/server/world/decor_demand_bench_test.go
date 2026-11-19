package world

import "testing"

var decorDemandSink uint64

func BenchmarkDecorRegionDemand(b *testing.B) {
	for _, mode := range []string{"construction", "sparse", "all"} {
		b.Run(mode, func(b *testing.B) {
			c := newChunk(0, 0)
			s := &prefetchSource{center: c, cached: 3}
			for i := range s.shaped {
				s.shaped[i].fill[0] = uint16(i + 1)
			}
			var states, calls uint64
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.calls = s.calls[:0]
				s.peak.Store(0)
				r := newDecorRegion(s, c, worldMinY, worldHeight)
				states += uint64(r.near[12].blockAt(0, worldMinY, 0))
				switch mode {
				case "sparse":
					for range 8 {
						states += uint64(r.terrainAt(-32, worldMinY, -32))
					}
				case "all":
					for i := range r.near {
						x, z := (int32(i%5)-2)*16, (int32(i/5)-2)*16
						states += uint64(r.terrainAt(x, worldMinY, z))
					}
				}
				calls += uint64(len(s.calls))
			}
			b.ReportMetric(float64(calls)/float64(b.N), "terrainOf/op")
			decorDemandSink = states
		})
	}
}
