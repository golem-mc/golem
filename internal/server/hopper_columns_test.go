package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestItemColumnsKeepEveryIntersectingBox(t *testing.T) {
	w, other := &world.World{}, &world.World{}
	live := &itemEntity{w: w, item: &entity.Item{X: -0.01, Y: 80, Z: 1.99}}
	s := &Server{items: []*itemEntity{
		live,
		{w: w, item: &entity.Item{X: 10.5, Z: 10.5, Dead: true}},
		{w: other, item: &entity.Item{X: 20.5, Z: 20.5}},
	}}
	s.indexItemColumns(w)
	for _, x := range []int32{-1, 0} {
		for _, z := range []int32{1, 2} {
			if !s.itemsOverColumn(w, x, z) {
				t.Errorf("box crossing column boundaries missed %d,%d", x, z)
			}
		}
	}
	for _, pos := range [][2]int32{{1, 1}, {10, 10}, {20, 20}} {
		if s.itemsOverColumn(w, pos[0], pos[1]) {
			t.Errorf("indexed absent, dead or other-world item at %v", pos)
		}
	}
	// Column membership must still leave height and liveness to the exact query.
	if got := s.itemEntitiesIn(w, -1, 0, 1, 0, 1, 2); len(got) != 0 {
		t.Fatalf("exact query included item at another height: %v", got)
	}
	live.item.Dead = true
	if got := s.itemEntitiesIn(w, -1, 79, 1, 0, 81, 2); len(got) != 0 {
		t.Fatalf("exact query included dead item: %v", got)
	}
	if !s.itemsOverColumn(other, 30, 30) {
		t.Error("another dimension must fall back to the full scan")
	}
	s.dropItemColumns()
	if !s.itemsOverColumn(w, 30, 30) {
		t.Error("outside the block-entity pass must fall back to the full scan")
	}
}

func TestItemColumnsIncludeItemsRegisteredDuringPass(t *testing.T) {
	w := &world.World{}
	s := &Server{}
	s.indexItemColumns(w)
	it := &itemEntity{w: w, item: &entity.Item{X: 4.5, Y: 80, Z: 5.5}}
	s.registerItem(it)
	if !s.itemsOverColumn(w, 4, 5) {
		t.Fatal("later hopper cannot see an item registered during the pass")
	}
	if got := s.itemEntitiesIn(w, 4, 79, 5, 5, 81, 6); len(got) != 1 || got[0] != it {
		t.Fatalf("exact query missed registered item: %v", got)
	}
	s.indexItemColumns(&world.World{})
	if s.itemsOverColumn(s.itemCols.w, 4, 5) {
		t.Error("next dimension retained the previous dimension's columns")
	}
}

func TestBlockEntityPeekReadsSlotsWithStoredCompounds(t *testing.T) {
	for _, kind := range []world.BlockEntityKind{world.KindShelf, world.KindChiseledBookshelf} {
		be := &world.BlockEntity{
			Kind:   kind,
			Slots:  []protocol.ItemStack{{}, {ItemID: int32(registry.ItemStone), Count: 3}},
			Stored: map[int]nbt.Compound{0: {"id": nbt.String("minecraft:stone"), "count": nbt.Int(1)}},
		}
		b := blockEntitySlots{be: be}
		for slot := -1; slot <= len(be.Slots); slot++ {
			stack := b.Slot(slot)
			id, count := b.Peek(slot)
			if id != stack.ItemID || count != stack.Count {
				t.Errorf("%s slot %d: peek %d,%d differs from slot %v", kind, slot, id, count, stack)
			}
		}
	}
}

func TestItemColumnsIncludeTeleportsDuringPass(t *testing.T) {
	w := &world.World{}
	it := &itemEntity{w: w, item: &entity.Item{X: 0.5, Y: 80, Z: 0.5}}
	s := &Server{items: []*itemEntity{it}}
	s.indexItemColumns(w)
	s.teleportItem(it, player.Position{X: -4.01, Y: 80, Z: 5.99}, "test")
	for _, x := range []int32{-5, -4} {
		for _, z := range []int32{5, 6} {
			if !s.itemsOverColumn(w, x, z) {
				t.Errorf("later hopper missed teleported item's column %d,%d", x, z)
			}
		}
	}
}
