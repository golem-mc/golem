package terrain

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

type countedMaterialNoise struct {
	Noise
	calls int
}

func (n *countedMaterialNoise) Get3D(x, y, z float64) float32 {
	n.calls++
	return float32(x + y + z)
}

func TestMaterialNoiseSuppliers(t *testing.T) {
	rs := NewRandomState(Overworld, 12345)
	nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
	c := newMaterialContext(rs.Materials, nc, nc.volume, nil, nil)
	n := &countedMaterialNoise{}
	stack := new(NoiseStackBuilder).Add(n, 1, 1).Build()
	// Exercise the threshold helper as well as direct supplier requests.
	rs.noises["minecraft:calcite"] = stack
	a := noiseThreshold("calcite", 0, 100, false)(c)
	b := noiseThreshold("calcite", 5, 100, false)(c)
	d := noiseThreshold("calcite", 0, 100, true)(c)
	check := func(want int) {
		t.Helper()
		if !a() || !b() || !d() {
			t.Fatal("unexpected threshold result")
		}
		if n.calls != want {
			t.Fatalf("noise sampled %d times, want %d", n.calls, want)
		}
	}
	c.updateXZ(2, 3, 0, 0)
	c.updateY(1, 1, noWater, 4)
	check(2)
	check(2)
	c.updateY(2, 1, noWater, 5)
	check(3) // Only the 3D supplier expires when Y changes.
	c.updateXZ(3, 4, 0, 0)
	check(5)
	other := &countedMaterialNoise{}
	c.noise(new(NoiseStackBuilder).Add(other, 1, 1).Build(), false)()
	if other.calls != 1 || n.calls != 5 {
		t.Fatal("distinct noises shared a supplier")
	}
}

func TestMaterialSurfaceConcurrent(t *testing.T) {
	const count = 4
	rs := NewRandomState(Overworld, 12345)
	biomes := &goldenBiomeSource{names: goldenBiomes["overworld"], zoom: levelgen.BiomeZoomSeed(rs.Seed), melt: strings.Repeat("0", 256)}
	var chunks [count]*ProtoChunk
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range count {
		chunks[i], _ = buildGoldenChunk("overworld", int32(i), 0, 70, 3)
		wg.Go(func() {
			<-start
			nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(int32(i), 0))
			rs.Materials.BuildSurface(chunks[i], int32(i), 0, nc, biomes, nil)
		})
	}
	close(start)
	wg.Wait()
	serial := NewRandomState(Overworld, rs.Seed)
	for i, got := range chunks {
		want, _ := buildGoldenChunk("overworld", int32(i), 0, 70, 3)
		nc := NewNoiseChunk(serial, nil, serial.ChunkVolume(int32(i), 0))
		serial.Materials.BuildSurface(want, int32(i), 0, nc, biomes, nil)
		if !slices.Equal(got.blocks, want.blocks) || got.tops != want.tops {
			t.Errorf("chunk %d differs from serial generation", i)
		}
	}
}

type countedMaterialBiomes struct {
	goldenBiomeSource
	biome, calls int
}

func (b *countedMaterialBiomes) At(_, _, _ int32) int {
	b.calls++
	return b.biome
}

func TestMaterialBiomePruning(t *testing.T) {
	for _, tt := range []struct {
		name     string
		possible []int
		paths    []string
		want     bool
		lazy     bool
	}{
		{"absent", []int{0, 1}, []string{"sulfur_caves"}, false, false},
		{"all", []int{0, 1}, []string{"plains", "forest"}, true, false},
		{"subset", []int{0}, []string{"plains", "forest"}, true, false},
		{"empty", []int{}, []string{"plains"}, false, false},
		{"missing registry entry", []int{0}, []string{"unknown"}, false, false},
		{"mixed", []int{0, 1}, []string{"plains"}, true, true},
		{"unknown possibilities", nil, []string{"plains"}, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			biomes := &countedMaterialBiomes{names: []string{"plains", "forest", "sulfur_caves"}}
			c := &materialContext{biomes: biomes, possibleBiomes: tt.possible}
			test := isBiome(tt.paths...)(c)
			if biomes.calls != 0 {
				t.Fatal("compilation sampled a biome")
			}
			c.updateY(1, 1, noWater, 70)
			for range 2 {
				if got := test(); got != tt.want {
					t.Fatalf("condition = %v, want %v", got, tt.want)
				}
			}
			wantCalls := 0
			if tt.lazy {
				wantCalls = 1
			}
			if biomes.calls != wantCalls {
				t.Fatalf("biome samples = %d, want %d", biomes.calls, wantCalls)
			}
			biomes.biome = 1
			c.updateY(2, 1, noWater, 69)
			want := tt.want && !tt.lazy
			if got := test(); got != want {
				t.Fatalf("condition after Y update = %v, want %v", got, want)
			}
			if tt.lazy {
				wantCalls++
			}
			if biomes.calls != wantCalls {
				t.Fatalf("biome samples after Y update = %d, want %d", biomes.calls, wantCalls)
			}
		})
	}
}

func TestMaterialSurfaceBiomePruning(t *testing.T) {
	rs := NewRandomState(Overworld, 12345)
	sys := *rs.Materials
	sys.rule = sequence(ifTrue(isBiome("plains"), block(Dirt)), block(Stone))
	build := func(possible []int) (*ProtoChunk, int) {
		chunk, _ := buildGoldenChunk("overworld", 0, 0, 70, 3)
		biomes := &countedMaterialBiomes{names: []string{"plains"}}
		nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
		sys.BuildSurface(chunk, 0, 0, nc, biomes, possible)
		return chunk, biomes.calls
	}
	want, lazyCalls := build(nil)
	got, prunedCalls := build([]int{0})
	if !slices.Equal(got.blocks, want.blocks) || got.tops != want.tops {
		t.Fatal("biome pruning changed blocks or heightmaps")
	}
	// BuildSurface still reads one surface biome per column for extensions.
	if prunedCalls != 256 || lazyCalls <= prunedCalls {
		t.Fatalf("biome samples: pruned %d, lazy %d; want 256 and more than 256", prunedCalls, lazyCalls)
	}
}

type extensionMaterialNoise struct{ Noise }

func (*extensionMaterialNoise) Get3D(_, _, _ float64) float32 { return 1 }

// Empty sequences place nothing but must still walk columns and build extensions.
func TestMaterialAllFalseRoots(t *testing.T) {
	rs := NewRandomState(Overworld, 12345)
	for _, name := range []string{"plains", "frozen_ocean", "eroded_badlands"} {
		t.Run(name, func(t *testing.T) {
			var contexts [2]*materialContext
			var chunks [2]*ProtoChunk
			for i := range chunks {
				sys := *rs.Materials
				// Force pillars so skipping an extension is observable.
				pillar := new(NoiseStackBuilder).Add(&extensionMaterialNoise{}, 1, 1).Build()
				sys.badlandsSurface, sys.badlandsPillar, sys.badlandsPillarRoof = pillar, pillar, pillar
				sys.icebergSurface, sys.icebergPillar, sys.icebergPillarRoof = pillar, pillar, pillar
				sys.rule = func(c *materialContext) ruleFn {
					contexts[i] = c
					if i == 0 {
						return sequence()(c)
					}
					return func(int32, int32, int32) (Material, bool) { return 0, false }
				}
				chunks[i], _ = buildGoldenChunk("overworld", 0, 0, 62, 0)
				biomes := &countedMaterialBiomes{names: []string{name}}
				biomes.melt = strings.Repeat("0", 256)
				nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
				sys.BuildSurface(chunks[i], 0, 0, nc, biomes, []int{0})
				nc.Recycle()
				if biomes.calls != 256 {
					t.Fatalf("surface biome reads = %d, want 256", biomes.calls)
				}
			}
			if !slices.Equal(chunks[0].blocks, chunks[1].blocks) || chunks[0].tops != chunks[1].tops {
				t.Fatal("empty rule changed extension behavior")
			}
			a, b := contexts[0], contexts[1]
			if a.updXZ != 256 || a.updY <= 256 || a.updY != b.updY || a.x != 15 || a.z != 15 || a.y != rs.MinY || a.above != b.above || a.below != b.below || a.water != b.water {
				t.Fatal("empty rule skipped context updates")
			}
			before, _ := buildGoldenChunk("overworld", 0, 0, 62, 0)
			same := slices.Equal(chunks[0].blocks, before.blocks)
			if same != (name == "plains") {
				t.Fatalf("extension changes = %v for %s", !same, name)
			}
		})
	}
	for _, underFluid := range []bool{false, true} {
		sys := *rs.Materials
		var ctx *materialContext
		sys.rule = func(c *materialContext) ruleFn { ctx = c; return sequence()(c) }
		chunk, _ := buildGoldenChunk("overworld", 0, 0, 70, 3)
		nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(0, 0))
		if _, ok := sys.topMaterial(chunk, nc, &countedMaterialBiomes{names: []string{"plains"}}, 7, 65, 9, underFluid); ok {
			t.Fatal("empty top material placed a block")
		}
		nc.Recycle()
		water := int32(noWater)
		if underFluid {
			water = 66
		}
		if ctx.possibleBiomes != nil || ctx.updXZ != 1 || ctx.updY != 2 || ctx.x != 7 || ctx.y != 65 || ctx.z != 9 || ctx.water != water || ctx.above != 1 || ctx.below != 1 {
			t.Fatal("empty top material skipped context setup")
		}
	}
}
