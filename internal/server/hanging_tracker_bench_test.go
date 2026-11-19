package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// hangingBenchSpot is body i's cell on trackerBenchSpot's grid, a block
// above the ground.
func hangingBenchSpot(i int) (x, y, z int32) {
	fx, fz := trackerBenchSpot(i)
	return int32(fx), world.GroundLevel + 1, int32(fz)
}

// BenchmarkTickPaintingsTracked runs the whole painting tick over paintings
// whose survives poll never comes due, every one watched by every player.
func BenchmarkTickPaintingsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dims := registry.PaintingVariants[0]
	for i := range trackerBenchBodies {
		x, y, z := hangingBenchSpot(i)
		p := newPainting(s.nextEntity(), uuid.New(), w, x, y, z, 3, 0, dims.Width, dims.Height)
		p.checkInterval = -1 << 30
		s.registerPainting(p)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickPaintings()
	}
	b.StopTimer()
	if n := len(s.paintings); n != trackerBenchBodies {
		b.Fatalf("%d paintings after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickFramesTracked runs the whole item frame tick over frames
// whose survives poll never comes due, every one watched by every player.
func BenchmarkTickFramesTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, y, z := hangingBenchSpot(i)
		f := newFrame(s.nextEntity(), uuid.New(), w, x, y, z, 3, false)
		f.checkInterval = -1 << 30
		s.registerFrame(f)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickFrames()
	}
	b.StopTimer()
	if n := len(s.frames); n != trackerBenchBodies {
		b.Fatalf("%d frames after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickKnotsTracked runs the whole leash knot tick over knots whose
// survives poll never comes due, every one watched by every player.
func BenchmarkTickKnotsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, y, z := hangingBenchSpot(i)
		k, _, _ := s.tieKnot(w, x, y, z)
		k.checkInterval = -1 << 30
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickKnots()
	}
	b.StopTimer()
	if n := len(s.knots); n != trackerBenchBodies {
		b.Fatalf("%d knots after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickFixturesTracked runs the fixtures' tracking pass over text
// displays, every one watched by every player.
func BenchmarkTickFixturesTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		ent := fixtureOfTag(s, w, summonTag(nbt.Compound{}, entity.FixtureTextDisplay, x, float64(world.GroundLevel)+1, z))
		s.registerFixture(ent)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickFixtureTracking()
	}
	b.StopTimer()
	if n := len(s.fixtures); n != trackerBenchBodies {
		b.Fatalf("%d fixtures after the run, want %d", n, trackerBenchBodies)
	}
}
