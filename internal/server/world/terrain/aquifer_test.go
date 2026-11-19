package terrain

import (
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
