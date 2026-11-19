package terrain

import (
	"slices"
	"strconv"
	"testing"
)

func TestCarvingMaskVisit(t *testing.T) {
	for _, height := range []int32{1, 65, 376} {
		t.Run(strconv.Itoa(int(height)), func(t *testing.T) {
			m := NewCarvingMask(-63, -64+height)
			var got [][4]int32
			visit := func(x, z, bottom, top int32) { got = append(got, [4]int32{x, z, bottom, top}) }
			m.Visit(visit)
			if len(got) != 0 {
				t.Fatal("empty mask visited", got)
			}
			// Global indices include full words, word boundaries, column
			// boundaries, sparse distant marks and the final valid bit.
			for _, span := range [][2]int{{0, 0}, {62, 193}, {int(height) - 2, int(height) + 2}, {16*int(height) - 2, 16*int(height) + 2}, {256*int(height) - 1, 256*int(height) - 1}} {
				for i := max(0, span[0]); i <= min(256*int(height)-1, span[1]); i++ {
					col := i / int(height)
					m.Carve(int32(col>>4), m.minY+int32(i%int(height)), int32(col&15))
				}
			}
			var want [][4]int32
			for x := range int32(16) {
				for z := range int32(16) {
					for y := m.minY; y <= m.maxY; y++ {
						if !m.get(x, y, z) {
							continue
						}
						bottom := y
						for y < m.maxY && m.get(x, y+1, z) {
							y++
						}
						want = append(want, [4]int32{x, z, bottom, y})
					}
				}
			}
			m.Visit(visit)
			if !slices.Equal(got, want) {
				t.Fatalf("runs = %v, want %v", got, want)
			}
		})
	}
}
