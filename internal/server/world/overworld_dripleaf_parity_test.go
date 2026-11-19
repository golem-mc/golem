package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestOverworldDripleafStemOnMagmaVanilla pins a clay-pool dripleaf stem whose
// support is a magma block: the shape update only schedules the stem's death,
// so the finished chunk still holds it. Frozen vanilla 26.3 seed 12345.
func TestOverworldDripleafStemOnMagmaVanilla(t *testing.T) {
	t.Parallel()
	for _, p := range []struct{ x, y, z int32 }{{331, 44, -273}} {
		g := NewOverworldGenerator(12345)
		c := newChunk(p.x>>4, p.z>>4)
		g.Generate(c)
		s := c.BlockAt(p.x&15, p.y, p.z&15)
		if b := registry.BlockOf(s); b != registry.BlockBigDripleafStem {
			t.Errorf("(%d,%d,%d) is %s, want big_dripleaf_stem", p.x, p.y, p.z, b.Name())
		}
	}
}
