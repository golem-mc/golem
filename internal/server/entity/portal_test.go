package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestPortalArrivalPositionClearsAnObstruction(t *testing.T) {
	from, to := newTestWorld(), newTestWorld()
	for x := range int32(2) {
		for y := int32(64); y < 67; y++ {
			from.set(x, y, 0, rules.NetherPortalX)
			to.set(x, y, 0, rules.NetherPortalX)
			if y < 66 {
				to.set(x, y, 1, rules.ObsidianState)
			}
		}
	}
	entry := world.SpawnPos{Y: 64}
	pos := [3]float64{1, 64, 0.9}
	got := PortalArrivalPosition(from, to, entry, entry, pos, 1, 2, false)
	want := [3]float64{1, 64, 0.5}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("arrival = %v, want %v", got, want)
		}
	}
	// The collision search deliberately excludes bodies larger than four blocks.
	got = PortalArrivalPosition(from, to, entry, entry, pos, 5, 2, false)
	if got != pos {
		t.Fatalf("oversize arrival = %v, want %v", got, pos)
	}
}

func TestPortalArrivalPositionCentersBeforeClearingAnObstruction(t *testing.T) {
	from, to := newTestWorld(), newTestWorld()
	for x := range int32(2) {
		for y := int32(64); y < 67; y++ {
			from.set(x, y, 0, rules.NetherPortalX)
			to.set(x, y, 0, rules.NetherPortalX)
			to.set(x, y, -1, rules.ObsidianState)
		}
	}
	entry := world.SpawnPos{Y: 64}
	got := PortalArrivalPosition(from, to, entry, entry, [3]float64{1, 64, 0.9}, 1.4, 2, true)
	// Centering at z=0.5 overlaps the rear wall; collision correction must follow it.
	want := [3]float64{1, 64, 0.7}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("arrival = %v, want %v", got, want)
		}
	}
}
