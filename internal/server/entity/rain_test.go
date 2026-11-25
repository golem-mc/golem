package entity

import (
	"slices"
	"testing"
)

type recordingRainWorld struct {
	testWorld
	wetY  int32
	reads [][3]int32
}

func (w *recordingRainWorld) RainingAt(x, y, z int32) bool {
	w.reads = append(w.reads, [3]int32{x, y, z})
	return y == w.wetY
}

func TestRainLandsOn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		feet  float64
		wetY  int32
		wet   bool
		reads [][3]int32
	}{
		{"dry same cell", 64.1, 65, false, [][3]int32{{-1, 64, 8}}},
		{"wet feet", 64.9, 64, true, [][3]int32{{-1, 64, 8}}},
		{"wet top", 64.9, 65, true, [][3]int32{{-1, 64, 8}, {-1, 65, 8}}},
		{"negative boundary", -1.25, -1, true, [][3]int32{{-1, -2, 8}, {-1, -1, 8}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &recordingRainWorld{wetY: tc.wetY}
			if got := rainLandsOn(w, -0.5, tc.feet, 8.5, 0.25); got != tc.wet {
				t.Errorf("rainLandsOn = %v, want %v", got, tc.wet)
			}
			if !slices.Equal(w.reads, tc.reads) {
				t.Errorf("queried cells = %v, want %v", w.reads, tc.reads)
			}
		})
	}
}
