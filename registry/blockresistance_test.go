package registry

import "testing"

func TestBlockExplosionResistanceMatchesGeneratedTable(t *testing.T) {
	for b := range Block(BlockCount) {
		want, ok := blockExplosionResistance[b.Name()]
		if !ok {
			t.Fatalf("%s has no generated resistance", b.Name())
		}
		if got := b.ExplosionResistance(); got != want {
			t.Errorf("%s resistance = %g, want %g", b.Name(), got, want)
		}
	}
	for _, b := range []Block{BlockCount, ^Block(0)} {
		if got := b.ExplosionResistance(); got != 0 {
			t.Errorf("unknown block %d resistance = %g, want 0", b, got)
		}
	}
}
