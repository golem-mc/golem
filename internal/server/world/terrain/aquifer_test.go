package terrain

import (
	"math"
	"strings"
	"testing"
)

// The column fixtures come from vanilla's NoiseBasedAquifer via SamplerGolden.
// Reorder their queries to exercise column and vertical-anchor invalidation,
// including negative coordinates and revisiting cells after an early return.
func TestAquiferTraversalOrder(t *testing.T) {
	chunks := map[string][]goldenRecord{}
	for _, r := range goldenOf(t, "aquifer") {
		if r.fields[0] == "overworld" {
			key := strings.Join(r.fields[:4], "/")
			chunks[key] = append(chunks[key], r)
		}
	}
	states := goldenStates{}
	for key, records := range chunks {
		t.Run(key, func(t *testing.T) {
			r := records[0]
			rs := states.get(t, r.fields[0], r.int(t, 1))
			cx, cz := int32(r.int(t, 2)), int32(r.int(t, 3))
			for _, interleaved := range []bool{false, true} {
				name := "ascending"
				if interleaved {
					name = "interleaved"
				}
				t.Run(name, func(t *testing.T) {
					n := NewNoiseChunk(rs, nil, rs.ChunkVolume(cx, cz))
					defer n.Recycle()
					v := n.Volume()
					density := n.SampleDensity()
					cols := make([][]Substance, len(records))
					for i := range cols {
						cols[i] = make([]Substance, v.SizeY)
					}
					query := func(i int, y int32) {
						r := records[i]
						x, z := int32(r.int(t, 4)), int32(r.int(t, 5))
						bx, by, bz := v.BlockX(x), v.BlockY(y), v.BlockZ(z)
						cols[i][y] = n.Aquifer().ComputeSubstance(bx, by, bz, float64(density[v.Index(x, y, z)]))
					}
					if interleaved {
						for y := range v.SizeY {
							// Alternate the bottom and top halves to jump anchors.
							j := y / 2
							if y%2 != 0 {
								j = v.SizeY - 1 - j
							}
							for i := range records {
								query(i, j)
							}
						}
					} else {
						for i := range records {
							for y := range v.SizeY {
								query(i, y)
							}
						}
					}
					for i, r := range records {
						if got := runs(cols[i], v.MinY); got != r.values[0] {
							t.Errorf("%s:\n got %s\nwant %s", r, got, r.values[0])
						}
					}
				})
			}
		})
	}
}

func TestAquiferSurfaceCacheEdges(t *testing.T) {
	sampler := &aquiferSurfaceSampler{}
	ctx := NewContext(nil)
	defer ctx.recycle()
	a := NewAquifer(ctx, &AquiferConfig{SurfaceLevel: sampler}, nil, NewVolume(16, 384, 16, -32, -64, -80), nil)
	if a.surfaceCache != nil {
		t.Error("precomputing the surface allocated the sparse cache")
	}

	// Inclusive quart-aligned bounds for chunk (-2, -5).
	const minX, maxX, minZ, maxZ = -48, -8, -96, -56
	for _, tc := range []struct {
		name              string
		x, z              int32
		sampleX, sampleZ  int32
		wantScalarSamples int
	}{
		{"min x min z", minX + 3, minZ + 1, minX, minZ, 0},
		{"max x min z", maxX + 3, minZ + 1, maxX, minZ, 0},
		{"min x max z", minX + 3, maxZ + 2, minX, maxZ, 0},
		{"max x max z", maxX + 3, maxZ + 2, maxX, maxZ, 0},
		{"below min x", minX - 1, minZ + 5, minX - 4, minZ + 4, 1},
		{"above max x", maxX + 4, minZ + 5, maxX + 4, minZ + 4, 1},
		{"below min z", minX + 5, minZ - 1, minX + 4, minZ - 4, 1},
		{"above max z", minX + 5, maxZ + 4, minX + 4, maxZ + 4, 1},
		{"same outside quart", minX - 2, minZ + 7, minX - 4, minZ + 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := int32(math.Floor(float64(sampler.Sample(ctx, tc.sampleX, 0, tc.sampleZ))))
			sampler.calls = 0
			if got := a.surfaceLevel(tc.x, tc.z); got != want {
				t.Errorf("surfaceLevel(%d, %d) = %d, want %d", tc.x, tc.z, got, want)
			}
			if sampler.calls != tc.wantScalarSamples {
				t.Errorf("scalar samples = %d, want %d", sampler.calls, tc.wantScalarSamples)
			}
		})
	}
}

type aquiferSurfaceSampler struct{ calls int }

func (s *aquiferSurfaceSampler) Sample(_ *Context, x, y, z int32) float32 {
	s.calls++
	return float32(x+y+31*z) - 0.25
}

func (s *aquiferSurfaceSampler) SampleVolume(c *Context, out []float32, v Volume) {
	sampleVolumeNaive(c, out, v, s)
}
