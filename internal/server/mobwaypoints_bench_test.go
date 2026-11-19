package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// BenchmarkMobWaypointSources reads 2000 mobs with attribute maps, none
// transmitting.
func BenchmarkMobWaypointSources(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range 2000 {
		x, z := trackerBenchSpot(i)
		id := s.spawnMobInDim(s.allDims()[0], entity.TypePig, x, float64(world.GroundLevel), z, int64(i+1), nil)
		s.mobsMu.Lock()
		m := s.mobByEntityIDLocked(s.allDims()[0].w, id).mob
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		s.mobsMu.Unlock()
	}
	b.ResetTimer()
	for b.Loop() {
		if n := len(s.mobWaypointSources()); n != 0 {
			b.Fatalf("%d transmitting mobs, want 0", n)
		}
	}
}
