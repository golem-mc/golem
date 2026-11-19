package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

func TestEndGatewayInsideUsesTheBoxAndMovement(t *testing.T) {
	w := newTestWorld()
	w.set(3, 90, 3, rules.EndGatewayState)
	for _, tc := range []struct {
		name     string
		from, to AABB
		want     bool
	}{
		{"head", NewAABB(3.25, 89, 3.25, 3.75, 90.975, 3.75), NewAABB(3.25, 89, 3.25, 3.75, 90.975, 3.75), true},
		{"flush", NewAABB(2, 90, 3, 3, 91, 4), NewAABB(2, 90, 3, 3, 91, 4), false},
		{"swept", NewAABB(2.375, 90, 3.375, 2.625, 90.25, 3.625), NewAABB(4.375, 90, 3.375, 4.625, 90.25, 3.625), true},
		{"miss", NewAABB(2, 91, 3, 2.5, 92, 3.5), NewAABB(5, 91, 3, 5.5, 92, 3.5), false},
		{"capped", NewAABB(-100, 90, 3, -99.5, 91, 3.5), NewAABB(100, 90, 3, 100.5, 91, 3.5), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y, z, ok := EndGatewayInside(w, tc.from, tc.to)
			if ok != tc.want || ok && (x != 3 || y != 90 || z != 3) {
				t.Fatalf("gateway = (%d, %d, %d), %v; want found=%v at (3, 90, 3)", x, y, z, ok, tc.want)
			}
		})
	}
}
