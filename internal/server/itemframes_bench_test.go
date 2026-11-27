package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkPollFramesDue runs the survives poll over frames that all come due
// on the same tick, laid flat on the ground without overlapping.
func BenchmarkPollFramesDue(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			for i := range n {
				x, z := int32(i%90-45), int32(i/90-45)
				f := newFrame(s.nextEntity(), uuid.New(), w, x, world.GroundLevel, z, 1, true)
				f.checkInterval = 100
				s.registerFrame(f)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, f := range s.frames {
					f.checkInterval = 100
				}
				s.pollFrames()
			}
			b.StopTimer()
			if len(s.frames) != n {
				b.Fatalf("%d frames after the run, want %d", len(s.frames), n)
			}
		})
	}
}
