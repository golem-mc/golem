package terrain

import (
	"fmt"
	"strings"
	"testing"
)

func TestBaseColumnGolden(t *testing.T) {
	states := goldenStates{}
	g := readGolden(t, "testdata/column_golden.txt")
	if len(g["column"]) == 0 {
		t.Fatal("no column records")
	}
	for _, r := range g["column"] {
		rs := states.get(t, r.fields[0], r.int(t, 1))
		x, z, minY, height := int32(r.int(t, 2)), int32(r.int(t, 3)), int32(r.int(t, 4)), int32(r.int(t, 5))
		text := "null"
		if c := rs.BaseColumn(x, z, minY, height); c != nil {
			var parts []string
			start := 0
			for i, m := range c.Blocks {
				if i == len(c.Blocks)-1 || c.Blocks[i+1] != m {
					parts = append(parts, fmt.Sprintf("%s*%d", m.Name(), i-start+1))
					start = i + 1
				}
			}
			text = fmt.Sprintf("%d:%s", c.MinY, strings.Join(parts, ","))
		}
		if text != r.values[0] {
			t.Fatalf("%s\n got %s\nwant %s", r, text, r.values[0])
		}
		surface := rs.BaseHeight(x, z, minY, height, func(m Material) bool { return m != Air })
		floor := rs.BaseHeight(x, z, minY, height, func(m Material) bool { return m != Air && !m.IsFluid() })
		got := fmt.Sprintf("%d %d", surface, floor)
		if want := strings.Join(r.values[1:], " "); got != want {
			t.Fatalf("%s: heights %s, want %s", r, got, want)
		}
	}
}

// TestTopDefaultYMatchesDoFill pins the one-column sampler to the chunk fill.
func TestTopDefaultYMatchesDoFill(t *testing.T) {
	t.Parallel()
	rs := NewRandomState(Overworld, 12345)
	const cx, cz = 5, -7
	var want [16][16]int32
	for x := range want {
		for z := range want[x] {
			want[x][z] = rs.MinY
		}
	}
	nc := NewNoiseChunk(rs, nil, rs.ChunkVolume(cx, cz))
	defer nc.Recycle()
	density := nc.SampleDensity()
	defer nc.ReleaseDensity(density)
	nc.DoFill(density, func(x, y, z int32, s Substance) {
		if s == SubstanceDefault {
			want[x&15][z&15] = max(want[x&15][z&15], y)
		}
	})
	for x := int32(0); x < 16; x += 3 {
		for z := int32(0); z < 16; z += 3 {
			if got := rs.TopDefaultY(cx*16+x, cz*16+z); got != want[x][z] {
				t.Fatalf("column %d,%d: got %d, want %d", x, z, got, want[x][z])
			}
		}
	}
}
