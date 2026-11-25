package server

import (
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkArrowFrameSweep ticks a hundred stuck arrows, their chunks entity-ticking, in a world holding
// the frames given: the per-arrow sweep must not walk the world's whole
// frame list, so the tick cost grows with the arrows, not the arrows times
// the frames.
func BenchmarkArrowFrameSweep(b *testing.B) {
	for _, frames := range []int{0, 2000} {
		b.Run(strconv.Itoa(frames), func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			defer s.Shutdown()

			w := s.world
			s.framesMu.Lock()
			for i := range frames {
				f := newFrame(s.nextEntity(), uuid.New(), w,
					int32(i%50), world.GroundLevel+3, int32(i/50), protocol.FaceNorth, false)
				s.registerFrameLocked(f)
			}
			s.framesMu.Unlock()

			var arrows []*arrowEntity
			held := make(map[world.ChunkPos]bool)
			// The arrows stand clear of every frame, so the fix's filter
			// cuts each one's target list to nothing.
			for i := range 100 {
				it := newArrowEntity(s, w, 100.5+float64(i), world.GroundLevel+3, 100.5, 0, 0, 0, 0, 0,
					false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
					protocol.ItemStack{}, 0)
				it.arrow.InGround = true
				it.arrow.LastState = w.BlockAt(int32(math.Floor(it.arrow.X)), world.GroundLevel+3, 100)
				cp := world.ChunkPos{X: world.ChunkOf(it.arrow.X), Z: world.ChunkOf(it.arrow.Z)}
				if !held[cp] {
					held[cp] = true
					defer w.HoldEntityTicking(cp)()
				}
				arrows = append(arrows, it)
				// A stuck arrow counts its life to the discard; keep them
				// out of reach so the arrows survive every iteration.
				it.arrow.Life = -1 << 30
				s.registerArrow(it)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickArrows()
			}
			b.StopTimer()
			for _, it := range arrows {
				if !it.arrow.InGround || it.arrow.Life == -1<<30 {
					b.Fatalf("arrow lodged=%v life=%d: not ticked as a lodged arrow", it.arrow.InGround, it.arrow.Life)
				}
			}
		})
	}
}

// BenchmarkTickArrows runs the server's whole arrow tick, the step under the
// locks and the phase after them, over three hundred arrows in the held
// chunks of the flat world beside forty cows, no players in the world: a
// hundred stuck, two hundred hanging in the air with no gravity, none in
// reach of a body. Every arrow is kept from its discard, so each iteration
// ticks the same set.
func BenchmarkTickArrows(b *testing.B) { benchTickArrows(b, 300, 40, arrowsStill, 0) }

// BenchmarkTickArrowsHerd is BenchmarkTickArrows beside two thousand cows.
func BenchmarkTickArrowsHerd(b *testing.B) { benchTickArrows(b, 300, 2000, arrowsStill, 0) }

func BenchmarkTickArrowsOneLodgedInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(strconv.Itoa(cows), func(b *testing.B) {
			benchTickArrows(b, 1, cows, arrowsLodged, 0)
		})
	}
}

// BenchmarkTickArrowsFlying is BenchmarkTickArrows with every arrow flying
// sideways through air with no gravity, 2.5 blocks a tick, put back at its
// start before each tick so it never leaves the held chunks.
func BenchmarkTickArrowsFlying(b *testing.B) { benchTickArrows(b, 300, 40, arrowsFlying, 0) }

// BenchmarkTickArrowsFlyingBurning is BenchmarkTickArrowsFlying with every
// arrow on fire.
func BenchmarkTickArrowsFlyingBurning(b *testing.B) { benchTickArrows(b, 300, 40, arrowsBurning, 0) }

// BenchmarkTickArrowsMannequins runs flying arrows beside forty mannequins,
// with the larger case adding 1,960 distant bodies to the target snapshot.
func BenchmarkTickArrowsMannequins(b *testing.B) {
	for _, count := range []int{40, 2000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			benchTickArrows(b, 300, 0, arrowsFlying, count)
		})
	}
}

type arrowsMotion int

const (
	arrowsStill arrowsMotion = iota
	arrowsFlying
	arrowsBurning
	arrowsLodged
)

func benchTickArrows(b *testing.B, arrows, cows int, motion arrowsMotion, mannequins int) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	w := dim.w
	var releases []func()
	// Entity-ticking holds: a bare Acquire leaves the entity gate closed with
	// no player in the world, and the arrows would stay dormant.
	for cx := int32(-2); cx < 2; cx++ {
		for cz := int32(-2); cz < 2; cz++ {
			releases = append(releases, w.HoldEntityTicking(world.ChunkPos{X: cx, Z: cz}))
		}
	}
	b.Cleanup(func() {
		for _, release := range releases {
			release()
		}
		s.Shutdown()
	})
	w.FlushLighting()

	for i := range cows {
		s.spawnMobInDim(dim, entity.TypeCow, float64(i%8)*3-12, world.GroundLevel, float64(i/8%8)*3-12, int64(i), nil)
	}
	for i := range mannequins {
		x, z := float64(i%8)*3-12, float64(i/8%8)*3-12
		if i >= 40 {
			x += 4096
		}
		s.registerMannequin(&mannequinEntity{
			// Nearby bodies enter the broad sweep but stay below the actual hit line.
			mannequin: entity.NewMannequin(s.nextEntity(), x, world.GroundLevel+7.5, z),
			w:         w,
		})
	}
	type start struct{ x, z float64 }
	var flying, grounded []*entity.Arrow
	var starts []start
	for i := range arrows {
		x, z := float64(i%20)*1.5-15+0.25, float64(i/20)*1.5-11+0.25
		y := float64(world.GroundLevel + 10)
		if motion == arrowsLodged {
			y = world.GroundLevel - 0.5
		}
		it := newArrowEntity(s, w, x, y, z, 0, 0, 0, 0, 0,
			false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
			protocol.ItemStack{}, 0)
		if motion == arrowsFlying || motion == arrowsBurning {
			it.arrow.NoGravity = true
			flying = append(flying, it.arrow)
			starts = append(starts, start{x, z})
		} else if motion == arrowsLodged || i%3 == 0 {
			it.arrow.InGround = true
			grounded = append(grounded, it.arrow)
			it.arrow.LastState = w.BlockAt(int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z)))
		} else {
			it.arrow.NoGravity = true
		}
		it.arrow.Life = -1 << 30
		s.registerArrow(it)
	}
	s.mobsMu.Lock()
	herd := len(dim.mobs.herd)
	s.mobsMu.Unlock()
	if herd != cows {
		b.Fatalf("%d mobs in the herd, want %d", herd, cows)
	}

	// The flight crosses two cells along x and one or two along z.
	const vx, vz = 2, 1.5
	fire := 0
	if motion == arrowsBurning {
		fire = 100
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i, a := range flying {
			a.X, a.Y, a.Z = starts[i].x, world.GroundLevel+10, starts[i].z
			a.VX, a.VY, a.VZ = vx, 0, vz
			a.FireTicks = fire
		}
		s.tickArrows()
	}
	b.StopTimer()
	for _, a := range grounded {
		if !a.InGround || a.Life != -1<<30+b.N {
			b.Fatalf("grounded arrow: InGround=%v Life=%d, want lodged with Life %d", a.InGround, a.Life, -1<<30+b.N)
		}
	}
	for i, a := range flying {
		if a.X == starts[i].x && a.Z == starts[i].z {
			b.Fatal("flying arrow did not move: arrows are dormant")
		}
	}
	s.arrowsMu.Lock()
	n := len(s.arrows)
	s.arrowsMu.Unlock()
	if n != arrows {
		b.Fatalf("%d arrows after the run, want %d", n, arrows)
	}
}
