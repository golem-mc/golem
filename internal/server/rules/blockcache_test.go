package rules

import (
	"math/rand"
	"sync"
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

// TestPrefilledBlocksKeepExposure checks concurrent reads from a prefilled box
// match the underlying reader.
func TestPrefilledBlocksKeepExposure(t *testing.T) {
	raw := scatterBlocks{seed: 7}
	p := NewPrefilledBlocks(raw, -12, 52, -12, 13, 77, 13)
	defer p.Release()

	type sample struct{ x, y, z, want float64 }
	rng := rand.New(rand.NewSource(1))
	samples := make([]sample, 200)
	for i := range samples {
		x, y, z := rng.Float64()*20-10, 64+rng.Float64()*16-8, rng.Float64()*20-10
		samples[i] = sample{x, y, z, ExplosionExposure(raw, x, y, z, x+0.98, y+0.98, z+0.98, 0.5, 64, 0.5)}
	}

	got := make([]float64, len(samples))
	var wg sync.WaitGroup
	for w := range 4 {
		start, end := w*50, (w+1)*50
		wg.Go(func() {
			for i := start; i < end; i++ {
				s := samples[i]
				got[i] = ExplosionExposure(p, s.x, s.y, s.z, s.x+0.98, s.y+0.98, s.z+0.98, 0.5, 64, 0.5)
			}
		})
	}
	wg.Wait()
	for i, s := range samples {
		if got[i] != s.want {
			t.Fatalf("sample %d at %v,%v,%v: prefilled %v, raw %v", i, s.x, s.y, s.z, got[i], s.want)
		}
	}
}
