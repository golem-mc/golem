package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestHopperColumnSeesItemRegisteredDuringPass(t *testing.T) {
	s := &Server{}
	w := new(world.World)
	s.indexItemColumns(w)
	if s.itemsOverColumn(w, 0, 0) {
		t.Fatal("empty hopper column reported an item")
	}
	if scanned := len(s.itemCols.scanned); scanned != 0 {
		t.Errorf("empty query scanned %d chunks, want 0", scanned)
	}

	stack := protocol.ItemStack{ItemID: int32(registry.ItemDirt), Count: 1}
	s.spawnItem(w, stack, 0.5, 65, 0.5, 0, 0, 0, 0)
	if !s.itemsOverColumn(w, 0, 0) {
		t.Fatal("hopper column missed the drop registered during the pass")
	}
}
