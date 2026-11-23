package terrain

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// A depth above the 384-entry stack buffer fills every height and consumes
// the same draws as a plain per-height loop.
func TestFillWidthFactorsOversizeDepth(t *testing.T) {
	c := &canyonCarver{widthSmoothness: 3}
	const depth = 500
	got := make([]float32, depth)
	r := levelgen.NewLegacyRandom(42)
	c.fillWidthFactors(got, r)

	ref := levelgen.NewLegacyRandom(42)
	wf := float32(1)
	for i := range depth {
		if i == 0 || ref.NextIntN(3) == 0 {
			a, b := ref.NextFloat(), ref.NextFloat()
			wf = 1 + float32(a*b)
		}
		if want := float32(wf * wf); got[i] != want {
			t.Fatalf("factor %d = %v, want %v", i, got[i], want)
		}
	}
	if r.NextLong() != ref.NextLong() {
		t.Fatal("next draw differs")
	}
}
