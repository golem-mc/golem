package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestFilledSlotsFollowsWrites(t *testing.T) {
	b := newBlockEntity(KindChest, 0, 0, 0)
	if b.FilledSlots() != 0 {
		t.Fatalf("fresh mask %b", b.FilledSlots())
	}
	b.Put(3, protocol.ItemStack{ItemID: 1, Count: 2})
	if b.FilledSlots() != 1<<3 {
		t.Fatalf("after put: %b", b.FilledSlots())
	}
	b.Put(3, protocol.ItemStack{})
	if b.FilledSlots() != 0 {
		t.Fatalf("after empty put: %b", b.FilledSlots())
	}
	b.Slots[5] = protocol.ItemStack{ItemID: 1, Count: 1}
	b.Slots[7] = protocol.ItemStack{ItemID: 1, Count: 1}
	b.recomputeFilled()
	if b.FilledSlots() != 1<<5|1<<7 {
		t.Fatalf("after recompute: %b", b.FilledSlots())
	}
}

func TestFilledSlotsFollowsDecodeAndLoot(t *testing.T) {
	src := newBlockEntity(KindChest, 0, 0, 0)
	src.Put(4, protocol.ItemStack{ItemID: 1, Count: 3})
	back := newBlockEntity(KindChest, 0, 0, 0)
	back.loadFrom(src.EncodeNBT())
	if back.FilledSlots() != 1<<4 {
		t.Fatalf("after decode: %b", back.FilledSlots())
	}

	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	w.SetLootFill(func(_ *World, _, _, _ int32, _ string, _ int64, slots []protocol.ItemStack) bool {
		slots[9] = protocol.ItemStack{ItemID: 1, Count: 1}
		return true
	})
	loot := newBlockEntity(KindChest, 0, 0, 0)
	loot.LootTable = "minecraft:chests/test"
	w.UnpackLoot(loot)
	if loot.FilledSlots() != 1<<9 {
		t.Fatalf("after loot: %b", loot.FilledSlots())
	}
}
