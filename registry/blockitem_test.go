package registry

import "testing"

func TestBlockItem(t *testing.T) {
	if item, ok := BlockHangingRoots.Item(); !ok || item != ItemHangingRoots {
		t.Errorf("hanging_roots item = %d %v", item, ok)
	}
	if _, ok := BlockWater.Item(); ok {
		t.Error("water has an item")
	}
	if _, ok := Block(BlockCount).Item(); ok {
		t.Error("an out-of-range block has an item")
	}
}
