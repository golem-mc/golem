package world

import (
	"bytes"
	"slices"
	"sync/atomic"
	"testing"
)

// countingGenerator is a flat generator that counts the chunks it generates.
type countingGenerator struct {
	*FlatGenerator
	generated atomic.Int32
}

func (g *countingGenerator) Generate(c *Chunk) {
	g.generated.Add(1)
	g.FlatGenerator.Generate(c)
	c.pendingGenerated = []posKey{{1, GroundLevel, 2}, {3, GroundLevel + 1, 4}}
}

// TestAGenerationFixtureGeneratesOnceAndHandsEachWorldACopy checks that two
// worlds reading one fixture chunk cost one generation, and that a write to
// one world's copy stays out of the other's.
func TestAGenerationFixtureGeneratesOnceAndHandsEachWorldACopy(t *testing.T) {
	t.Parallel()
	gen := &countingGenerator{FlatGenerator: NewFlatGenerator()}
	f := NewGenerationFixture(gen)
	a, b := newTestWorld(t, NewFlatGenerator()), newTestWorld(t, NewFlatGenerator())
	a.GenerateFrom(f)
	b.GenerateFrom(f)

	pos := ChunkPos{X: 2, Z: 3}
	x, y, z := pos.X*16+5, int32(GroundLevel-1), pos.Z*16+7
	ca, _ := a.Acquire(pos)
	defer a.Release(pos)
	want := encodeWhole(ca)
	visits := slices.Clone(ca.pendingGenerated)
	if !slices.Equal(visits, []posKey{{1, GroundLevel, 2}, {3, GroundLevel + 1, 4}}) {
		t.Fatalf("first copy's generated visits = %v", visits)
	}
	if !a.SetBlock(x, y, z, airState) {
		t.Fatal("could not dig into the first world's copy")
	}
	ca.pendingGenerated[0][1]++
	cb, _ := b.Acquire(pos)
	defer b.Release(pos)

	if ca == cb {
		t.Fatal("both worlds hold the same chunk")
	}
	if !bytes.Equal(want, encodeWhole(cb)) {
		t.Error("second copy's encoded chunk differs from the original")
	}
	if !slices.Equal(visits, cb.pendingGenerated) {
		t.Errorf("second copy's generated visits = %v, want %v", cb.pendingGenerated, visits)
	}
	if got := b.BlockAt(x, y, z); got != stoneState {
		t.Errorf("second world's block = %d, want stone %d: the first world's dig reached it", got, stoneState)
	}
	if got := gen.generated.Load(); got != 1 {
		t.Errorf("generated %d times, want once", got)
	}
}
