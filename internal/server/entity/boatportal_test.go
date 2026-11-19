package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// vanilla: Entity.checkInsideBlocks
func TestBoatPortalContactUsesTheHullBox(t *testing.T) {
	for _, portal := range []uint16{rules.NetherPortalX, rules.NetherPortalZ, rules.EndPortalState} {
		w := newTestWorld()
		w.set(0, 64, 0, portal)
		for _, tc := range []struct {
			name        string
			x, y, z     float64
			nether, end bool
		}{
			{"center", 0.5, 64, 0.5, true, true},
			{"side", 1.5, 64, 0.5, true, true},
			{"corner", 1.5, 64, 1.5, true, true},
			{"top", 0.5, 63.5, 0.5, true, false},
			{"below_end_shape", 0.5, 63.75, 0.5, true, false},
			{"above_end_shape", 0.5, 64.875, 0.5, true, false},
			{"flush_end_shape", 0.5, 64.75, 0.5, true, false},
			{"inside_end_shape", 0.5, 64.5, 0.5, true, true},
			{"flush_above", 0.5, 65, 0.5, false, false},
			{"flush_below", 0.5, 64 - BoatHeight, 0.5, false, false},
			{"flush_side", 1 + BoatHalfWidth, 64, 0.5, false, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				b := NewBoat(1, tc.x, tc.y, tc.z, 0)
				var want uint16
				if portal == rules.EndPortalState && tc.end || portal != rules.EndPortalState && tc.nether {
					want = portal
				}
				if got := b.PortalState(w); got != want {
					t.Errorf("portal state = %d, want %d", got, want)
				}
			})
		}
	}
}
