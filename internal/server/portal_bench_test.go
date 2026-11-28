package server

import (
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkHorsePortalCrossing repeats a horse's crossing to an existing exit.
func BenchmarkHorsePortalCrossing(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	to := s.dimensionByName(world.NetherName)
	chunk := world.ChunkPos{}
	to.w.Acquire(chunk)
	b.Cleanup(func() { to.w.Release(chunk) })
	for _, dim := range []*dimension{s.allDims()[0], to} {
		for x := int32(3); x <= 6; x++ {
			for y := int32(100); y <= 104; y++ {
				state := rules.ObsidianState
				if x > 3 && x < 6 && y > 100 && y < 104 {
					state = rules.NetherPortalX
				}
				dim.w.SetBlock(x, y, 4, state)
			}
		}
	}
	id := s.spawnMobInDim(s.allDims()[0], entity.TypeHorse, 4.5, 101, 4.5, 1, nil)
	ent := s.mobByEntityIDLocked(w, id)
	s.allDims()[0].mobs.forget(ent.mob)
	s.allDims()[0].mobs.setHerd(nil)
	tr := mobPortalTravel{ent: ent, kind: rules.PortalNether}
	// Wake the destination once, outside the measured repeated crossings.
	s.finishMobTravel(tr)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range to.mobs.herd {
			to.mobs.forget(m)
		}
		to.mobs.setHerd(nil)
		s.finishMobTravel(tr)
	}
}

// BenchmarkBoatPortalCrossing repeats an off-center hull's trip to an existing exit.
func BenchmarkBoatPortalCrossing(b *testing.B) {
	s, _ := newCommandBenchServer(b, 0)
	to := s.dimensionByName(world.NetherName)
	chunk := world.ChunkPos{}
	to.w.Acquire(chunk)
	b.Cleanup(func() { to.w.Release(chunk) })
	for _, dim := range []*dimension{s.allDims()[0], to} {
		for x := int32(3); x <= 6; x++ {
			for y := int32(100); y <= 104; y++ {
				state := rules.ObsidianState
				if x > 3 && x < 6 && y > 100 && y < 104 {
					state = rules.NetherPortalX
				}
				dim.w.SetBlock(x, y, 4, state)
			}
		}
	}
	it := &boatEntity{boat: entity.NewBoat(s.nextEntity(), 4.5, 101, 4.7, 0), w: s.allDims()[0].w, kind: registry.EntityTypeOakBoat}
	s.finishBoatTravel(it, rules.PortalNether)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.boats = nil
		s.finishBoatTravel(it, rules.PortalNether)
	}
}

// BenchmarkCrossPortalsIdleHerd is the portal pass over 2000 cows in a
// dimension with no portal block.
func BenchmarkCrossPortalsIdleHerd(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeCow, 1+float64(i%50), world.GroundLevel, 1+float64(i/50), int64(i), nil)
	}
	h := s.herdOf(w)
	if len(h.herd) != 2000 {
		b.Fatalf("herd holds %d mobs, want 2000", len(h.herd))
	}
	t := &mobDimTick{s: s, dim: dim, h: h, w: w}
	fx := &mobEffects{}
	t.crossPortals(fx)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		t.crossPortals(fx)
	}
}
