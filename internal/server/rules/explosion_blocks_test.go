package rules

import (
	"cmp"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// groundWorld is stone below y=64 and a wall at x=12, over air.
type groundWorld struct{}

func (groundWorld) BlockAt(x, y, z int32) uint16 {
	if y < 64 || (x == 12 && y < 70) {
		return registry.BlockStone.DefaultState()
	}
	return 0
}

// explosionBlocksMap keeps vanilla's float operations and a map of reached
// cells, independent of the production bitset and its reach bound.
// vanilla: ServerExplosion.calculateExplodedPositions
func explosionBlocksMap(w BlockReader, rng *rand.Rand, x, y, z, radius float64, override func(bx, by, bz int32) (float64, bool)) [][3]int32 {
	destroyed := map[[3]int32]struct{}{}
	for gx := range 16 {
		for gy := range 16 {
			for gz := range 16 {
				if gx != 0 && gx != 15 && gy != 0 && gy != 15 && gz != 0 && gz != 15 {
					continue
				}
				dx := float64(float32(gx)/15*2 - 1)
				dy := float64(float32(gy)/15*2 - 1)
				dz := float64(float32(gz)/15*2 - 1)
				length := math.Sqrt(dx*dx + dy*dy + dz*dz)
				dx, dy, dz = dx/length, dy/length, dz/length
				intensity := float32(radius) * (0.7 + float32(rng.Float32()*0.6))
				px, py, pz := x, y, z
				for intensity > 0 {
					bx, by, bz := int32(math.Floor(px)), int32(math.Floor(py)), int32(math.Floor(pz))
					state := w.BlockAt(bx, by, bz)
					resistance, has := 0.0, false
					if override != nil {
						resistance, has = override(bx, by, bz)
					}
					if !has {
						resistance, has = BlastResistance(state)
					}
					if has {
						intensity -= float32((float32(resistance) + 0.3) * 0.3)
					}
					if intensity > 0 {
						destroyed[[3]int32{bx, by, bz}] = struct{}{}
					}
					px += dx * 0.30000001192092896
					py += dy * 0.30000001192092896
					pz += dz * 0.30000001192092896
					intensity -= 0.22500001
				}
			}
		}
	}
	out := make([][3]int32, 0, len(destroyed))
	for c := range destroyed {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b [3]int32) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]), cmp.Compare(a[2], b[2]))
	})
	return out
}

func TestExplosionBlocksMatchesMapRayCast(t *testing.T) {
	// A resistance under -0.3 lets rays outrun the bitset's reach.
	runaway := func(bx, by, bz int32) (float64, bool) { return -0.9, by > 62 }
	cases := []struct {
		name     string
		radius   float64
		x, y, z  float64
		override func(bx, by, bz int32) (float64, bool)
	}{
		{"tnt", 4, 8.5, 64.06, 8.5, nil},
		{"small", 1, -3.2, 66.5, 20.9, nil},
		{"big", 12, 0.5, 66, 0.5, nil},
		{"wind", 1.2, 5.5, 64.5, 5.5, func(bx, by, bz int32) (float64, bool) { return -0.3, true }},
		{"runaway", 4, 8.5, 72, 8.5, runaway},
		// The first step's greatest X component straddles a cell boundary
		// only when the grid components round to float before normalizing.
		{"positive_boundary", 0.34, 0.7013244978332901, 72.5, 0.5, nil},
		{"negative_boundary", 0.34, 0.2986755021667099, 72.5, 0.5, nil},
		{"fractional_resistance", 4.1, -0.125, 67.0625, -0.875, func(int32, int32, int32) (float64, bool) { return 0.123456789, true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for seed := int64(1); seed <= 3; seed++ {
				refRNG := rand.New(rand.NewSource(seed))
				rng := rand.New(rand.NewSource(seed))
				want := explosionBlocksMap(groundWorld{}, refRNG, c.x, c.y, c.z, c.radius, c.override)
				got := ExplosionBlocks(groundWorld{}, rng, c.x, c.y, c.z, c.radius, c.override)
				if !slices.Equal([][3]int32(got), want) {
					t.Fatalf("seed %d: %d cells, want %d (or different cells)", seed, len(got), len(want))
				}
				if len(want) == 0 {
					t.Fatal("empty reference")
				}
				// One nextFloat per boundary ray, regardless of resistance
				// or the number of steps the remaining power can afford.
				raysRNG := rand.New(rand.NewSource(seed))
				for range 16*16*16 - 14*14*14 {
					raysRNG.Float32()
				}
				if next := raysRNG.Int63(); rng.Int63() != next || refRNG.Int63() != next {
					t.Fatalf("seed %d: RNG consumption differs from 1352 ray rolls", seed)
				}
			}
		})
	}
}

func BenchmarkExplosionBlocks(b *testing.B) {
	for _, impl := range []struct {
		name string
		run  func(rng *rand.Rand)
	}{
		{"bitset", func(rng *rand.Rand) { ExplosionBlocks(groundWorld{}, rng, 8.5, 64.06, 8.5, 4, nil) }},
		{"map", func(rng *rand.Rand) { explosionBlocksMap(groundWorld{}, rng, 8.5, 64.06, 8.5, 4, nil) }},
	} {
		b.Run(impl.name, func(b *testing.B) {
			rng := rand.New(rand.NewSource(1))
			b.ReportAllocs()
			for range b.N {
				impl.run(rng)
			}
		})
	}
}

type blastReadCounter struct {
	BlockReader
	reads map[[3]int32]int
}

func (w blastReadCounter) BlockAt(x, y, z int32) uint16 {
	w.reads[[3]int32{x, y, z}]++
	return w.BlockReader.BlockAt(x, y, z)
}

// caveGroundWorld replaces only groundWorld's air, preserving its obstacles.
type caveGroundWorld struct{}

func (caveGroundWorld) BlockAt(x, y, z int32) uint16 {
	state := (groundWorld{}).BlockAt(x, y, z)
	if state == registry.BlockAir.DefaultState() {
		return registry.BlockCaveAir.DefaultState()
	}
	return state
}

func TestBlastResistanceFollowsTheFluid(t *testing.T) {
	dry := registry.BlockOakSlab.DefaultState()
	wet, ok := registry.WithBool(dry, registry.PropWaterlogged, true)
	if !ok {
		t.Fatal("oak slab has no waterlogged property")
	}
	for _, tc := range []struct {
		name       string
		state      uint16
		resistance float64
		present    bool
	}{
		{"air", registry.BlockAir.DefaultState(), 0, false},
		{"cave_air", registry.BlockCaveAir.DefaultState(), 0, false},
		{"void_air", registry.BlockVoidAir.DefaultState(), 0, false},
		{"bubble_column", registry.BlockBubbleColumn.DefaultState(), 100, true},
		{"wet_slab", wet, 100, true},
		{"dry_slab", dry, 3, true},
		{"water", registry.BlockWater.DefaultState(), 100, true},
		{"lava", registry.BlockLava.DefaultState(), 100, true},
		{"outside_registry", registry.StateCount, 0, false},
		{"invalid", ^uint16(0), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, present := BlastResistance(tc.state); got != tc.resistance || present != tc.present {
				t.Fatalf("BlastResistance(%d) = %g, %v; want %g, %v", tc.state, got, present, tc.resistance, tc.present)
			}
		})
	}
	want := ExplosionBlocks(groundWorld{}, rand.New(rand.NewSource(1)), 8.5, 64.06, 8.5, 4, nil)
	got := ExplosionBlocks(caveGroundWorld{}, rand.New(rand.NewSource(1)), 8.5, 64.06, 8.5, 4, nil)
	if len(want) == 0 || !slices.Equal(got, want) {
		t.Fatalf("cave air: %d cells, want %d (or different cells)", len(got), len(want))
	}
}

type blastSourceCounter struct {
	rand.Source
	draws int
}

func (s *blastSourceCounter) Int63() int64 {
	s.draws++
	return s.Source.Int63()
}

type blastFirstCellReader struct {
	t      *testing.T
	source *blastSourceCounter
	reads  int
}

func (w *blastFirstCellReader) BlockAt(_, _, _ int32) uint16 {
	if w.reads == 0 && w.source.draws != 1 {
		w.t.Fatalf("first block read followed %d RNG draws, want one", w.source.draws)
	}
	w.reads++
	return registry.BlockStone.DefaultState()
}

// orderedBlastReader records first visits, independent of the cache's layout.
type orderedBlastReader struct {
	BlockReader
	visits [][3]int32
	seen   map[[3]int32]bool
}

func (w *orderedBlastReader) BlockAt(x, y, z int32) uint16 {
	pos := [3]int32{x, y, z}
	if !w.seen[pos] {
		w.seen[pos] = true
		w.visits = append(w.visits, pos)
	}
	return w.BlockReader.BlockAt(x, y, z)
}

type mixedBlastReader struct{ fluid bool }

func (w mixedBlastReader) BlockAt(x, y, z int32) uint16 {
	if (x+3*y+5*z)&7 == 0 {
		if w.fluid {
			return registry.BlockWater.DefaultState()
		}
		return registry.BlockStone.DefaultState()
	}
	if (x+y+z)&3 == 0 {
		return registry.BlockOakSlab.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
