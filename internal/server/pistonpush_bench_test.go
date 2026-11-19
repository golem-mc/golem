package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// BenchmarkPistonPushItemsAway measures repeated sweeps in one physics step,
// with resting items outside the piston's reach.
func BenchmarkPistonPushItemsAway(b *testing.B) {
	for _, n := range []int{0, 2000} {
		name := "empty"
		if n > 0 {
			name = "2000"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			for i := range n {
				s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1},
					float64(i%40)-20, float64(world.GroundLevel), float64(i/40)-25, 0, 0, 0, 0)
			}
			pp := &world.PistonPush{X: 30, Y: world.GroundLevel, Z: 30, Dir: 5, Extending: true, Source: true,
				Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}, Top: 1}
			s.setPistonGrid(s.allDims()[0], true)
			s.pushByPiston(w, pp)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.pushByPiston(w, pp)
			}
		})
	}
}

func BenchmarkPistonOrbCollection(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	for i := range 16 {
		s.orbs = append(s.orbs, &orbEntity{w: w, orb: &entity.Orb{ID: int32(i + 1), X: 1.75, Y: 64, Z: 0.5, VX: 0.1}})
	}
	pp := &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Bounce: true, Boxes: []world.CollisionBox{{MinX: 0, MinY: 0, MinZ: 0, MaxX: 1, MaxY: 1, MaxZ: 1}}}
	if moves := s.collectOrbsPistonPush(s.allDims()[0], pp); len(moves) != 16 {
		b.Fatalf("collected %d bodies", len(moves))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if moves := s.collectOrbsPistonPush(s.allDims()[0], pp); len(moves) != 16 {
			b.Fatal("lost bodies")
		}
	}
}

// BenchmarkPistonPushCartsAndCrystals measures a whole push with empty lists,
// 256 of each kind outside the sweep, or those plus four of each inside it.
func BenchmarkPistonPushCartsAndCrystals(b *testing.B) {
	for _, bc := range []struct {
		name  string
		away  int
		sweep int
	}{{"none", 0, 0}, {"away", 256, 0}, {"sweep", 256, 4}} {
		b.Run(bc.name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			for i := range bc.away + bc.sweep {
				x := 8.5
				if i >= bc.away {
					x = 1.75
				}
				s.minecarts = append(s.minecarts, &minecartEntity{w: w, cart: entity.NewMinecart(int32(i+1), x, 64, 0.5)})
				s.endCrystals = append(s.endCrystals, &endCrystalEntity{w: w, crystal: entity.NewEndCrystal(int32(i+1+bc.away+bc.sweep), x, 64, 0.5)})
			}
			pp := &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}}
			reset := func() {
				for i := bc.away; i < bc.away+bc.sweep; i++ {
					s.minecarts[i].cart.X = 1.75
					s.endCrystals[i].crystal.X = 1.75
				}
			}
			// Warm the destination chunks held by teleports before timing the push.
			s.pushByPiston(w, pp)
			reset()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.pushByPiston(w, pp)
				reset()
			}
		})
	}
}

// BenchmarkPistonPushFixtures runs a whole piston push in a world holding 256
// displays and markers and a wide interaction, plus 4 cushions: in the sweep,
// where the push breaks them and each is put back, away from it, where they
// allow the grid to be reused, or none at all. The grid is active throughout.
func BenchmarkPistonPushFixtures(b *testing.B) {
	for _, bc := range []struct {
		name     string
		cushions int
		x        float64
	}{{"sweep", 4, 2}, {"away", 4, 8}, {"none", 0, 0}} {
		b.Run(bc.name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			w.SetBoolRule(world.RuleEntityDrops, false)
			kinds := []string{entity.FixtureBlockDisplay, entity.FixtureItemDisplay, entity.FixtureTextDisplay, entity.FixtureMarker}
			for i := range 256 {
				s.fixtures = append(s.fixtures, fixtureOfTag(s, w, summonTag(nil, kinds[i%len(kinds)], float64(i%16)+0.5, 70, float64(i/16)+0.5)))
			}
			s.fixtures = append(s.fixtures, fixtureOfTag(s, w, summonTag(nbt.Compound{"width": nbt.Float(1024), "height": nbt.Float(1)}, entity.FixtureInteraction, 1.75, 64, 0.5)))
			var cushions []*fixtureEntity
			for i := range bc.cushions {
				ent := fixtureOfTag(s, w, summonTag(nil, entity.FixtureCushion, bc.x, 64+float64(i)*0.25, 0.5))
				cushions = append(cushions, ent)
				s.fixtures = append(s.fixtures, ent)
			}
			pp := &world.PistonPush{X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true, Bounce: true, Boxes: []world.CollisionBox{{MinX: 0, MinY: 0, MinZ: 0, MaxX: 1, MaxY: 1, MaxZ: 1}}}
			s.setPistonGrid(s.allDims()[0], true)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.pushByPiston(w, pp)
				for _, ent := range cushions {
					if ent.dead {
						ent.dead = false
						s.fixtures = append(s.fixtures, ent)
					}
				}
			}
		})
	}
}

// BenchmarkPistonPushHerd runs a whole piston push in a world holding 2000
// cows, none of them near the ride.
func BenchmarkPistonPushHerd(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%40)-20, 200, float64(i/40)-25, int64(i), nil)
	}
	pp := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 1, Extending: true, Source: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}, Top: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.pushByPiston(w, pp)
	}
}

// BenchmarkPistonPushHerdLevel is BenchmarkPistonPushHerd with the cows on the
// piston's level, inside a physics step so the grid path runs.
func BenchmarkPistonPushHerdLevel(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%40)-20, 64, float64(i/40)-25, int64(i), nil)
	}
	dim.mobs.pistonGrid.active = true
	pp := &world.PistonPush{X: 100, Y: 64, Z: 100, Dir: 1, Extending: true, Source: true, Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}}, Top: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.pushByPiston(w, pp)
	}
}
