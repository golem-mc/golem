package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type scatterBlocks struct{ seed int64 }

func (s scatterBlocks) BlockAt(x, y, z int32) uint16 {
	h := rand.New(rand.NewSource(s.seed + int64(x)*73856093 ^ int64(y)*19349663 ^ int64(z)*83492791)).Intn(5)
	if h == 0 {
		return registry.BlockStone.DefaultState()
	}
	return 0
}

// TestCachedBlocksKeepExposure checks the cache changes no exposure.
func TestCachedBlocksKeepExposure(t *testing.T) {
	raw := scatterBlocks{seed: 7}
	c := NewCachedBlocks(raw, 0.5, 64, 0.5, 9)
	defer c.Release()
	rng := rand.New(rand.NewSource(1))
	for range 300 {
		x, y, z := rng.Float64()*30-15, 64+rng.Float64()*20-10, rng.Float64()*30-15
		want := ExplosionExposure(raw, x, y, z, x+0.98, y+0.98, z+0.98, 0.5, 64, 0.5)
		if got := ExplosionExposure(c, x, y, z, x+0.98, y+0.98, z+0.98, 0.5, 64, 0.5); got != want {
			t.Fatalf("box at %v,%v,%v: cached %v, raw %v", x, y, z, got, want)
		}
	}
}
