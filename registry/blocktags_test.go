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

// TestSignalSourceAndSkullTablesMatchNames checks the per-block tables against
// the name sets for every state.
func TestSignalSourceAndSkullTablesMatchNames(t *testing.T) {
	for s := range uint16(StateCount) {
		name, _, _ := BlockStateName(s)
		if IsRedstoneSignalSourceState(s) != RedstoneSignalSource[name] {
			t.Fatalf("signal source mismatch for state %d (%s)", s, name)
		}
		if IsSkullBlockState(s) != skullTypes[BlockTypes[name]] {
			t.Fatalf("skull mismatch for state %d (%s)", s, name)
		}
	}
}

func BenchmarkSignalSourceByState(b *testing.B) {
	s := BlockObserver.DefaultState()
	for range b.N {
		IsRedstoneSignalSourceState(s)
	}
}

// TestBlockMethodsMatchNameForms checks the Block predicates against the name
// forms kept for callers that hold only a name.
func TestBlockMethodsMatchNameForms(t *testing.T) {
	for b := range Block(BlockCount) {
		name := b.Name()
		if b.IsReplaceable() != IsReplaceable(name) || b.IsShulkerBox() != NameInTag(name, idTagShulkerBoxes) ||
			b.IsPreventMobSpawningInside() != IsPreventMobSpawningInside(name) {
			t.Errorf("%s: block and name predicates disagree", name)
		}
		if b.IsBars() != BarsBlocks[name] || b.IsFence() != NameInTag(name, TagIDFences) {
			t.Errorf("%s: bars or fence predicate disagrees with its source", name)
		}
	}
	if BlockStone.IsReplaceable() || !BlockAir.IsReplaceable() {
		t.Error("replaceable must hold for air and not for stone")
	}
}
