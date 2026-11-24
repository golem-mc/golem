package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkEntityScaffoldingSupport(b *testing.B) {
	const y = 64
	scaffold, _ := registry.WithInt(registry.BlockScaffolding.DefaultState(), registry.PropDistance, 0)
	for _, tc := range []struct {
		name  string
		state uint16
		feetY float64
	}{
		{name: "stone", state: registry.BlockStone.DefaultState(), feetY: y + 0.5},
		{name: "below_plate", state: scaffold, feetY: y + 0.5},
		{name: "at_plate", state: scaffold, feetY: y + 1 - 5e-6},
	} {
		w := newTestWorld()
		w.set(1, y, 0, tc.state)
		b.Run("drowned/"+tc.name, func(b *testing.B) {
			m := NewMob(TypeDrowned, 1, 0.5, tc.feetY, 0.5, 1)
			g := newDrownedBeachGoal(m)
			for b.Loop() {
				g.valid(w, 1, y, 0)
			}
		})
		b.Run("bullet/"+tc.name, func(b *testing.B) {
			bullet := NewThrown(1, ThrownShulkerBullet, 0.5, tc.feetY, 0.5, 0, 0, 0, 0, 1)
			target := ArrowTarget{EntityID: 2, MinX: 5, MinY: y + 3, MaxX: 6, MaxY: y + 4, MaxZ: 1}
			leg := bulletFlight{targetID: target.EntityID, dir: int8(faceEast), steps: 20}
			bullet.Bullet = new(bulletFlight)
			for b.Loop() {
				*bullet.Bullet = leg
				bullet.bulletMoved(w, &target)
			}
		})
	}
}
