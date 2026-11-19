package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkSpinTargetScan scans a spin's swept box with no target in it,
// including worlds with mannequins outside the box.
func BenchmarkSpinTargetScan(b *testing.B) {
	for _, count := range []int{0, 200} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			for i := range count {
				s.registerMannequin(mannequinOfTag(s, w,
					summonTag(nbt.Compound{}, mannequinEntityID, float64(i)+10, 64, 10)))
			}
			c := &client{srv: s, player: &player.Player{EntityID: s.nextEntity()}}
			c.dim.Store(s.allDims()[0])
			box := [6]float64{0.2, 64, 0.2, 0.8, 64.6, 0.8}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if c.attackSpinTarget(box) {
					b.Fatal("a body outside the swept box was hit")
				}
			}
		})
	}
}

// BenchmarkTickSpinAttack sweeps a doubled player's spin beside a herd,
// without hitting it so each iteration keeps the spin in flight.
func BenchmarkTickSpinAttack(b *testing.B) {
	for _, mobs := range []int{0, 100} {
		b.Run(strconv.Itoa(mobs), func(b *testing.B) {
			s, _ := newCommandBenchServer(b, 0)
			y := float64(world.GroundLevel)
			for i := range mobs {
				s.spawnMobInDim(s.allDims()[0], entity.TypeCow, 4.5, y, 4.5, int64(i), nil)
			}
			p := &player.Player{EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: 0.5, Y: y, Z: 0.5})
			p.Attributes(func(a *player.Attributes) { a.SetBase(registry.AttributeScale, 2) })
			c := &client{srv: s, player: p, spin: spinSession{ticks: 2, last: p.Position()}}
			c.dim.Store(s.allDims()[0])
			c.tickSpinAttack()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				c.spin.ticks = 2
				c.tickSpinAttack()
			}
		})
	}
}

// BenchmarkTickSpinAttackWallCollision measures an airborne spin and a wall collision,
// with and without an item overlapping the player's swept box.
func BenchmarkTickSpinAttackWallCollision(b *testing.B) {
	for _, collision := range []bool{false, true} {
		for _, item := range []bool{false, true} {
			name := "air"
			if collision {
				name = "wall"
			}
			if item {
				name += "/item"
			} else {
				name += "/empty"
			}
			b.Run(name, func(b *testing.B) {
				s, w := newTrackerBenchServer(b)
				for _, c := range s.playerClients() {
					c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
				}
				c := s.playerClients()[0]
				pos := c.player.Position()
				if item {
					s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1},
						pos.X, pos.Y, pos.Z, 0, 0, 0, -1)
				}
				c.horizontalCollision.Store(collision)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					c.spin = spinSession{ticks: rules.SpinAttackTicks, last: pos}
					c.tickSpinAttack()
				}
			})
		}
	}
}
