package registry

import "testing"

func TestItemBlock(t *testing.T) {
	if b, ok := ItemWheatSeeds.Block(); !ok || b != BlockWheat {
		t.Errorf("wheat seeds place %v, %v", b, ok)
	}
	if b, ok := ItemStone.Block(); !ok || b != BlockStone {
		t.Errorf("stone places %v, %v", b, ok)
	}
	if _, ok := ItemDiamond.Block(); ok {
		t.Error("diamond places a block")
	}
	if _, ok := Item(-1).Block(); ok {
		t.Error("item -1 places a block")
	}
	for i, name := range Items {
		want, wantOK := BlockByName(BlockOfItem(name))
		if got, ok := Item(i).Block(); ok != wantOK || got != want {
			t.Errorf("%s: Block() = %v, %v; want %v, %v", name, got, ok, want, wantOK)
		}
	}
}
