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

// A canyon in a 500-block range carves at heights past the 384-entry stack
// buffer, so doCarve must take the heap fallback.
func TestDoCarveOversizeDepth(t *testing.T) {
	const_ := func(v float32) floatProvider { return func(*carverRandom) float32 { return v } }
	c := &canyonCarver{
		widthSmoothness:             3,
		horizontalRadiusFactor:      const_(1),
		verticalRadiusDefaultFactor: 1,
	}
	g := carveRange{minY: 0, depth: 500}
	out := NewCarvingMask(g.minY, g.minY+g.depth-1)
	c.doCarve(g, 0, 0, 7, 8, 450, 8, 4, 0, 0, 10, 20, out)
	found := false
	for y := g.minY + 385; y < g.minY+g.depth; y++ { // width-factor index y-minY-1 >= 384
		for x := range int32(16) {
			for z := range int32(16) {
				found = found || out.Get(x, y, z)
			}
		}
	}
	if !found {
		t.Fatal("no block carved at a width-factor index of 384 or more")
	}
}
