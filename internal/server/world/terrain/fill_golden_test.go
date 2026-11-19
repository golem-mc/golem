package terrain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fnv64 is the FNV-1a hash of the float bits, each as four big-endian bytes.
func fnv64(values []float32) string {
	h := uint64(0xcbf29ce484222325)
	for _, v := range values {
		bits := floatBits(v)
		for shift := 24; shift >= 0; shift -= 8 {
			h ^= uint64(byte(bits >> uint(shift)))
			h *= 0x100000001b3
		}
	}
	return fmt.Sprintf("%016x", h)
}

func TestGoldenFill(t *testing.T) {
	chunks := map[string][]goldenRecord{}
	var keys []string
	for _, kind := range []string{"fill", "fillcol", "aquifer", "aquiferflag"} {
		for _, r := range goldenOf(t, kind) {
			key := strings.Join(r.fields[:4], "/")
			if _, ok := chunks[key]; !ok {
				keys = append(keys, key)
			}
			chunks[key] = append(chunks[key], r)
		}
	}
	endChunks := 0
	for _, key := range keys {
		if strings.HasPrefix(key, "end/") {
			endChunks++
		}
	}
	if endChunks != 29 {
		t.Fatalf("got %d End chunks, want 29", endChunks)
	}
	states := goldenStates{}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			checkGoldenChunk(t, states, chunks[key])
		})
	}
}

// checkGoldenChunk shares one density sample and fill across all chunk assertions.
func checkGoldenChunk(t *testing.T, states goldenStates, records []goldenRecord) {
	t.Helper()
	r := records[0]
	rs := states.get(t, r.fields[0], r.int(t, 1))
	cx, cz := int32(r.int(t, 2)), int32(r.int(t, 3))
	n := NewNoiseChunk(rs, nil, rs.ChunkVolume(cx, cz))
	v := n.Volume()
	d := n.SampleDensity()
	cols := map[[2]int32][]Substance{}
	for _, r := range records {
		if r.kind == "aquifer" {
			cols[[2]int32{int32(r.int(t, 4)), int32(r.int(t, 5))}] = make([]Substance, v.SizeY)
		}
	}
	n.DoFill(d, func(x, y, z int32, s Substance) {
		if col := cols[[2]int32{x - v.MinX, z - v.MinZ}]; col != nil {
			col[y-v.MinY] = s
		}
	})
	for _, r := range records {
		switch r.kind {
		case "fill":
			if got := fmt.Sprintf("%d %d %d %d %d %d", v.SizeX, v.SizeY, v.SizeZ, v.MinX, v.MinY, v.MinZ); got != strings.Join(r.fields[4:10], " ") {
				t.Fatalf("%s: volume %s", r, got)
			}
			if got := fnv64(d); got != r.fields[10] || strconv.Itoa(len(d)) != r.values[0] {
				t.Errorf("%s: hash %s (%d values), want %s (%s)", r, got, len(d), r.fields[10], r.values[0])
			}
		case "fillcol":
			x, z := int32(r.int(t, 4)), int32(r.int(t, 5))
			for y := range v.SizeY {
				if got := bitsHex(d[v.Index(x, y, z)]); got != r.values[y] {
					t.Errorf("%s: y index %d got %s want %s", r, y, got, r.values[y])
					break
				}
			}
		case "aquifer":
			x, z := int32(r.int(t, 4)), int32(r.int(t, 5))
			if got := runs(cols[[2]int32{x, z}], v.MinY); got != r.values[0] {
				t.Errorf("%s:\n got %s\nwant %s", r, got, r.values[0])
			}
		case "aquiferflag":
			want := r.values[0] == "1"
			if got := n.Aquifer().ShouldScheduleFluidUpdate(); got != want {
				t.Errorf("%s: got %v want %v", r, got, want)
			}
		}
	}
}

var substanceNames = [...]string{"null", "air", "water", "lava"}

// runs formats a column top first as vanilla's golden does.
func runs(col []Substance, minY int32) string {
	var parts []string
	top := len(col) - 1
	for i, c := range slices.Backward(col) {
		if i == 0 || col[i-1] != c {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", substanceNames[c], minY+int32(top), minY+int32(i)))
			top = i - 1
		}
	}
	return strings.Join(parts, ",")
}
