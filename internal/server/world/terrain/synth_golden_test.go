package terrain

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

func TestGoldenPerlinSimplex(t *testing.T) {
	cache := map[string]*PerlinNoise{}
	for _, r := range goldenOf(t, "perlin") {
		seed := r.int(t, 0)
		p := NewPerlinNoise(levelgen.NewXoroshiroRandom(seed))
		checkBits(t, r, p.Get3D(r.float(t, 1), r.float(t, 2), r.float(t, 3)))
	}
	_ = cache
	for _, r := range goldenOf(t, "perlin2d") {
		p := NewPerlinNoise(levelgen.NewXoroshiroRandom(r.int(t, 0)))
		checkBits(t, r, p.Get2D(r.float(t, 1), r.float(t, 2)))
	}
	for _, r := range goldenOf(t, "smeared") {
		p := NewSmearedPerlinNoise(levelgen.NewXoroshiroRandom(r.int(t, 0)), r.float(t, 1))
		checkBits(t, r, p.Get3D(r.float(t, 2), r.float(t, 3), r.float(t, 4)))
	}
	for _, r := range goldenOf(t, "simplex2") {
		s := NewSimplexNoise(levelgen.NewXoroshiroRandom(r.int(t, 0)))
		checkBits(t, r, s.Get2D(r.float(t, 1), r.float(t, 2)))
	}
	for _, r := range goldenOf(t, "simplex3") {
		s := NewSimplexNoise(levelgen.NewXoroshiroRandom(r.int(t, 0)))
		checkBits(t, r, s.Get3D(r.float(t, 1), r.float(t, 2), r.float(t, 3)))
	}
}

func goldenStack(seed int64, kind string) *NoiseStack {
	r := levelgen.NewXoroshiroRandom(seed)
	var b NoiseStackBuilder
	if kind == "perlin" {
		b.Add(NewPerlinNoise(r), 1.0, 1.0).Add(NewPerlinNoise(r), 0.5, 0.25).Add(NewPerlinNoise(r), 4.2, 0.3)
	} else {
		b.Add(NewPerlinNoise(r), 1.0, 1.0).Add(NewSmearedPerlinNoise(r, 8.0), 0.5, 0.25)
	}
	return b.Build()
}

func TestGoldenNormalNoise(t *testing.T) {
	for _, r := range goldenOf(t, "normal") {
		seed := r.int(t, 0)
		name := r.fields[1]
		f := levelgen.NewXoroshiroRandom(seed).ForkPositional()
		n := NoiseByName("minecraft:" + name).Create(f.FromHashOf("minecraft:" + name))
		checkBits(t, r, n.Get3D(r.float(t, 2), r.float(t, 3), r.float(t, 4)))
	}
	for _, r := range goldenOf(t, "normallegacy") {
		name := r.fields[1]
		n := NoiseByName("minecraft:" + name).CreateForLegacyNetherBiome(levelgen.NewLegacyRandom(r.int(t, 0)))
		checkBits(t, r, n.Get3D(r.float(t, 2), r.float(t, 3), r.float(t, 4)))
	}
	for _, r := range goldenOf(t, "stack") {
		checkBits(t, r, goldenStack(r.int(t, 0), r.fields[1]).Get3D(r.float(t, 2), r.float(t, 3), r.float(t, 4)))
	}
}

// goldenVolumeNoise builds the noise a volume record names.
func goldenVolumeNoise(kind string, seed int64) Noise {
	switch kind {
	case "perlin":
		return NewPerlinNoise(levelgen.NewXoroshiroRandom(seed))
	case "smeared8":
		return NewSmearedPerlinNoise(levelgen.NewXoroshiroRandom(seed), 8.0)
	case "stackperlin":
		return goldenStack(seed, "perlin")
	}
	return goldenStack(seed, "mixed")
}

// volumeFields reads the volume and scales of a volume record.
func volumeFields(t *testing.T, r goldenRecord) (Volume, float64, float64, float32) {
	t.Helper()
	i := func(k int) int32 { return int32(r.int(t, k)) }
	v := Volume{i(2), i(3), i(4), i(5), i(6), i(7), i(8), i(9), i(10)}
	return v, r.float(t, 11), r.float(t, 12), float32(r.float(t, 13))
}

func TestGoldenNoiseVolume(t *testing.T) {
	for _, kind := range []string{"volume", "volumepoint"} {
		for _, r := range goldenOf(t, kind) {
			v, xz, y, amp := volumeFields(t, r)
			n := goldenVolumeNoise(r.fields[0], r.int(t, 1))
			out := make([]float32, v.Size())
			if kind == "volume" {
				n.AddToVolume(out, v, xz, y, amp)
			} else {
				idx := 0
				for z := range v.SizeZ {
					for x := range v.SizeX {
						for yy := range v.SizeY {
							out[idx] = float32(amp * n.Get3D(float64(v.BlockX(x))*xz, float64(v.BlockY(yy))*y, float64(v.BlockZ(z))*xz))
							idx++
						}
					}
				}
			}
			if len(r.values) != len(out) {
				t.Fatalf("%s: %d values, want %d", r, len(out), len(r.values))
			}
			for i, w := range r.values {
				if bitsHex(out[i]) != w {
					t.Errorf("%s: [%d] got %s want %s", strings.Join(r.fields[:3], " "), i, bitsHex(out[i]), w)
					break
				}
			}
		}
	}
}
