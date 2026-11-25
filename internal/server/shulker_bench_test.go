package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkShulkerTeamDefence ticks ten guards beside ten stationary enemies.
// Shots are removed between ticks so the herd and projectile counts stay fixed.
func BenchmarkShulkerTeamDefence(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	s.scoreboard.AddTeam("guards", nbt.String("guards"))
	for i := range 10 {
		x, z := float64(i%5)*3+0.5, float64(i/5)*6+0.5
		id := s.spawnMobInDim(s.allDims()[0], entity.TypeShulker, x, world.GroundLevel, z, int64(i), nil)
		ent := s.mobByEntityIDLocked(w, id)
		ent.mob.Persistence = true
		s.scoreboard.JoinTeam("guards", []string{ent.uuid.String()})
		id = s.spawnMobInDim(s.allDims()[0], entity.TypeZombie, x+1.5, world.GroundLevel, z, int64(i), nil)
		enemy := s.mobByEntityIDLocked(w, id).mob
		enemy.NoAI, enemy.Persistence = true, true
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
		s.thrown = s.thrown[:0]
		for _, m := range s.allDims()[0].mobs.herd {
			if m.Type == entity.TypeZombie {
				m.Health = 20
			}
		}
	}
}

// BenchmarkShulkerShellShoves ticks ten shells opening east through the ravager
// each one rides, repeating opening and closing cycles. An unrelated pig and
// twenty items are reset into each swept slab every tick.
func BenchmarkShulkerShellShoves(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	var shells, others []*entity.Mob
	var items []*itemEntity
	stone, _ := registry.ItemID("minecraft:stone")
	for i := range 10 {
		x, z := float64(i%5)*3+0.5, float64(i/5)*6+0.5
		id := s.spawnMobInDim(s.allDims()[0], entity.TypeShulker, x, world.GroundLevel, z, int64(i), nil)
		m := s.mobByEntityIDLocked(w, id).mob
		m.NoAI = true
		m.SetMetadataShulkerFace(4)
		shells = append(shells, m)
		carrier := s.mobByEntityIDLocked(w, s.spawnMobInDim(s.allDims()[0], entity.TypeRavager, x, world.GroundLevel, z, int64(i), nil)).mob
		carrier.NoAI, carrier.Persistence = true, true
		m.Vehicle = carrier
		other := s.mobByEntityIDLocked(w, s.spawnMobInDim(s.allDims()[0], entity.TypePig, x+1.2, world.GroundLevel, z, int64(i), nil)).mob
		other.NoAI, other.NoGravity, other.Persistence = true, true, true
		others = append(others, other)
		for range 2 {
			items = append(items, s.spawnItem(w, protocol.ItemStack{ItemID: stone, Count: 1}, x+1, world.GroundLevel+0.5, z, 0, 0, 0, 1000))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%20 == 0 {
			for _, m := range shells {
				if i%40 == 0 {
					m.SetShulkerPeek(100)
				} else {
					m.SetShulkerPeek(0)
				}
			}
		}
		for j, it := range items {
			_, _, _, maxX, _, _ := shells[j/2].BlastBox()
			it.item.X = maxX + entity.ItemHalfSize
		}
		for j, o := range others {
			_, _, _, maxX, _, _ := shells[j].BlastBox()
			o.X = maxX + 0.3
		}
		s.tickMobs()
	}
}

// BenchmarkShulkerBulletCollisions ticks twenty snowballs into twenty bullets.
// Restoring the input bodies each tick keeps collisions in every sample;
// restoration is included identically in the main and branch measurements.
func BenchmarkShulkerBulletCollisions(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	for i := range 20 {
		x, z := float64(i%5)*3+1.5, float64(i/5)*3+1.5
		for _, kind := range []entity.ThrownKind{entity.ThrownShulkerBullet, entity.ThrownSnowball} {
			sx, sy, vx := x, float64(world.GroundLevel+4), 0.0
			if kind == entity.ThrownSnowball {
				sx, sy, vx = x-1, sy+0.125, 1
			}
			s.standThrown(kind, w, sx, sy, z, vx, 0, 0, 0, 0, 0, uuid.Nil, int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{})
		}
	}
	all := append([]*thrownEntity(nil), s.thrown...)
	initial := make([]entity.Thrown, len(all))
	for i, it := range all {
		initial[i] = *it.throwable
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.thrown = append(s.thrown[:0], all...)
		for i, it := range all {
			*it.throwable = initial[i]
		}
		s.tickThrowables()
	}
	b.StopTimer()
	broken := 0
	for _, it := range all {
		if it.throwable.Kind == entity.ThrownShulkerBullet && it.throwable.Dead {
			broken++
		}
	}
	b.ReportMetric(float64(broken), "bullets-broken/op")
}

// BenchmarkShulkerShellCartAndThrowShoves ticks ten shells through repeated opening and
// closing cycles, with carts and snowballs reset over their swept slabs each tick.
func BenchmarkShulkerShellCartAndThrowShoves(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	var shells []*entity.Mob
	var carts []*minecartEntity
	var balls []*thrownEntity

	for i := range 10 {
		x, z := float64(i%5)*3+0.5, float64(i/5)*6+0.5
		id := s.spawnMobInDim(s.allDims()[0], entity.TypeShulker, x, world.GroundLevel, z, int64(i), nil)
		m := s.mobByEntityIDLocked(w, id).mob
		m.NoAI = true
		shells = append(shells, m)
		carts = append(carts, s.spawnMinecart(w, entity.MinecartPlain, x, world.GroundLevel+1, z))
		s.standThrown(entity.ThrownSnowball, w, x, world.GroundLevel+1, z, 0.2, 0.1, 0, 0, 0, 0, uuid.Nil, int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{})
		balls = append(balls, s.thrown[len(s.thrown)-1])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%20 == 0 {
			for _, m := range shells {
				if i%40 == 0 {
					m.SetShulkerPeek(100)
				} else {
					m.SetShulkerPeek(0)
				}
			}
		}
		for j, cart := range carts {
			_, _, _, _, top, _ := shells[j].BlastBox()
			cart.cart.Y = top
			balls[j].throwable.Y = top
		}
		s.tickMobs()
	}
}

// BenchmarkTickMobsShulkerField ticks 300 shulkers four blocks apart, after
// their first teleports have settled.
func BenchmarkTickMobsShulkerField(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	for i := range 300 {
		s.spawnMobInDim(s.allDims()[0], entity.TypeShulker, float64(i%20)*4-40+0.5, float64(world.GroundLevel), float64(i/20)*4-30+0.5, int64(i), nil)
	}
	for range 100 {
		s.tickMobs()
		s.ticksRun.Add(1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
		s.ticksRun.Add(1)
	}
}
