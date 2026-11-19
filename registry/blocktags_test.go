package registry

import "testing"

// TestBlockTagBitsetsMatchRegistry checks every block against every tag in
// BlockTags, in both directions, through the typed lookups.
func TestBlockTagBitsetsMatchRegistry(t *testing.T) {
	for tag, members := range BlockTags {
		id := BlockTagOf(tag)
		in := map[string]bool{}
		for _, m := range members {
			in[m] = true
		}
		for b := range Block(BlockCount) {
			if got := b.InTag(id); got != in[b.Name()] {
				t.Fatalf("%s in %s: bitset %v, registry %v", b.Name(), tag, got, in[b.Name()])
			}
			if got := inBlockTag(b.Name(), tag); got != in[b.Name()] {
				t.Fatalf("inBlockTag(%s, %s) = %v", b.Name(), tag, got)
			}
		}
	}
	if Block(BlockCount).InTag(0) || inBlockTag("minecraft:stone", "minecraft:no_such_tag") {
		t.Fatal("out-of-range state or unknown tag reported a member")
	}
}

func BenchmarkSignalSourceByState(b *testing.B) {
	s := BlockObserver.DefaultState()
	for range b.N {
		IsRedstoneSignalSourceState(s)
	}
}
