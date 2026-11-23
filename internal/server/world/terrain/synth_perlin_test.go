package terrain

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// A one-height volume retains addToVolume's dotXZ rounding, which differs
// from get's dotXYZ rounding. vanilla: PerlinNoise.addToVolume
func TestPerlinNoiseVolumeColumns(t *testing.T) {
	for _, seed := range []int64{0, 12345, -1} {
		for _, smeared := range []bool{false, true} {
			p := NewPerlinNoise(levelgen.NewXoroshiroRandom(seed))
			if smeared {
				p = NewSmearedPerlinNoise(levelgen.NewXoroshiroRandom(seed), 0.125)
			}
			for _, scale := range []float64{0, 0.007, 0.125, 0.249, 0.5, 1, 85.5515, -0.125} {
				for _, v := range []Volume{
					{3, 97, 2, -13, -64, 29, 4, 8, 4},
					{2, 33, 3, 16777210, 16777210, -16777220, 3, 1, 7},
					{2, 17, 2, -17, 9, 29, 1, -3, 1},
					{1, 0, 1, 0, 0, 0, 1, 1, 1},
					{2, 385, 2, 5, -64, 7, 1, 1, 1}, // dynamic path above 384
				} {
					t.Run(fmt.Sprintf("%d/smeared=%t/scale=%g/%v", seed, smeared, scale, v), func(t *testing.T) {
						out := make([]float32, v.Size())
						for i := range out {
							out[i] = float32(i%7-3) * 0.25
						}
						p.AddToVolume(out, v, 0.0371, scale, -0.75)
						for z := range v.SizeZ {
							for x := range v.SizeX {
								for y := range v.SizeY {
									i := v.Index(x, y, z)
									want := []float32{float32(i%7-3) * 0.25}
									p.AddToVolume(want, NewVolume(1, 1, 1, v.BlockX(x), v.BlockY(y), v.BlockZ(z)), 0.0371, scale, -0.75)
									if math.Float32bits(out[i]) != math.Float32bits(want[0]) {
										t.Fatalf("at %d,%d,%d: got %08x, want %08x", x, y, z, math.Float32bits(out[i]), math.Float32bits(want[0]))
									}
								}
							}
						}
					})
				}
			}
		}
	}
}
