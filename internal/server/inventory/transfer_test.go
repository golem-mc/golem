package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

type testBlock struct {
	kind  Kind
	slots []protocol.ItemStack
}

func (b *testBlock) Slot(i int) protocol.ItemStack       { return b.slots[i] }
func (b *testBlock) Put(i int, stack protocol.ItemStack) { b.slots[i] = stack }
func (b *testBlock) Stacks() []protocol.ItemStack        { return b.slots }
func (b *testBlock) Peek(i int) (int32, int32)           { return b.slots[i].ItemID, b.slots[i].Count }
func (b *testBlock) Filled() uint64 {
	var m uint64
	for i, s := range b.slots {
		if s.Count > 0 {
			m |= 1 << i
		}
	}
	return m
}
func (b *testBlock) Len() int   { return len(b.slots) }
func (b *testBlock) Kind() Kind { return b.kind }

func item(tb testing.TB, name string, count int32) protocol.ItemStack {
	tb.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		tb.Fatalf("no item %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: count}
}

func TestAddItemMergesThenFillsAndReportsIntoEmpty(t *testing.T) {
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	left, intoEmpty := AddItem(hopper, item(t, "minecraft:stone", 1), 0, false)
	if !left.Empty() || !intoEmpty || hopper.slots[0].Count != 1 {
		t.Fatalf("first pour: left %v, intoEmpty %v, slot %v", left, intoEmpty, hopper.slots[0])
	}
	hopper.slots[0].Count = 63
	left, intoEmpty = AddItem(hopper, item(t, "minecraft:stone", 3), 0, false)
	if !left.Empty() || intoEmpty || hopper.slots[0].Count != 64 || hopper.slots[1].Count != 2 {
		t.Fatalf("second pour: left %v, intoEmpty %v, slots %v", left, intoEmpty, hopper.slots[:2])
	}
}

func TestFurnaceFacesAndShulkerRefusal(t *testing.T) {
	furnace := &testBlock{kind: KindFurnace, slots: make([]protocol.ItemStack, 3)}
	if left, _ := AddItem(furnace, item(t, "minecraft:stone", 1), protocol.FaceNorth, true); left.Empty() {
		t.Error("the furnace's side took stone into its fuel slot")
	}
	if left, _ := AddItem(furnace, item(t, "minecraft:coal", 1), protocol.FaceNorth, true); !left.Empty() || furnace.slots[1].Empty() {
		t.Error("the furnace's side refused coal")
	}
	if left, _ := AddItem(furnace, item(t, "minecraft:stone", 1), protocol.FaceTop, true); !left.Empty() || furnace.slots[0].Empty() {
		t.Error("the furnace's top refused stone")
	}
	if canTakeThroughFace(furnace, furnace.slots[1], 1, protocol.FaceBottom) {
		t.Error("the furnace's bottom gave up its fuel")
	}

	box := &testBlock{kind: KindShulker, slots: make([]protocol.ItemStack, 27)}
	if left, _ := AddItem(box, item(t, "minecraft:red_shulker_box", 1), protocol.FaceTop, true); left.Empty() {
		t.Error("a shulker box took a shulker box")
	}
}

func TestEjectSkipsWhatTheTargetRefuses(t *testing.T) {
	hopper := &testBlock{kind: KindHopper, slots: []protocol.ItemStack{
		item(t, "minecraft:stone", 2), item(t, "minecraft:coal", 1), {}, {}, {},
	}}
	furnace := &testBlock{kind: KindFurnace, slots: make([]protocol.ItemStack, 3)}
	moved, _ := Eject(hopper, furnace, protocol.FaceNorth)
	if !moved || !hopper.slots[1].Empty() || hopper.slots[0].Count != 2 || furnace.slots[1].Count != 1 {
		t.Fatalf("moved %v, hopper %v, furnace %v", moved, hopper.slots[:2], furnace.slots)
	}
}

func TestDispenseGiveBackTakesItsSlotWhenItCan(t *testing.T) {
	dispenser := &testBlock{kind: KindDispenser, slots: make([]protocol.ItemStack, 9)}
	if left := DispenseGiveBack(dispenser, 4, item(t, "minecraft:bucket", 1)); !left.Empty() ||
		!dispenser.slots[4].Equal(item(t, "minecraft:bucket", 1)) {
		t.Fatalf("empty slot: left %v, slot %v", left, dispenser.slots[4])
	}
	dispenser.slots[4] = item(t, "minecraft:bone_meal", 63)
	if left := DispenseGiveBack(dispenser, 4, item(t, "minecraft:bone_meal", 1)); !left.Empty() ||
		dispenser.slots[4].Count != 64 {
		t.Fatalf("same item: left %v, slot %v", left, dispenser.slots[4])
	}
}

func TestDispenseGiveBackOverflowsIntoTheOtherSlots(t *testing.T) {
	dispenser := &testBlock{kind: KindDispenser, slots: make([]protocol.ItemStack, 9)}
	dispenser.slots[4] = item(t, "minecraft:dirt", 64)
	if left := DispenseGiveBack(dispenser, 4, item(t, "minecraft:bone_meal", 1)); !left.Empty() ||
		!dispenser.slots[0].Equal(item(t, "minecraft:bone_meal", 1)) {
		t.Fatalf("left %v, slots %v", left, dispenser.slots)
	}
}

func TestSuckReadsTheFurnaceBottomOutputFirst(t *testing.T) {
	furnace := &testBlock{kind: KindFurnace, slots: []protocol.ItemStack{
		item(t, "minecraft:stone", 1), item(t, "minecraft:coal", 1), item(t, "minecraft:iron_ingot", 2),
	}}
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	moved, intoEmpty := Suck(furnace, hopper)
	if !moved || !intoEmpty || !hopper.slots[0].Equal(item(t, "minecraft:iron_ingot", 1)) ||
		!hopper.slots[1].Empty() || furnace.slots[2].Count != 1 || furnace.slots[1].Count != 1 {
		t.Fatalf("moved %v intoEmpty %v, hopper %v, furnace %v", moved, intoEmpty, hopper.slots[:2], furnace.slots)
	}
}

func TestSuckStopsAtTheFirstMove(t *testing.T) {
	chest := &testBlock{kind: KindChest, slots: []protocol.ItemStack{
		item(t, "minecraft:dirt", 2), item(t, "minecraft:stick", 1), item(t, "minecraft:cobblestone", 1),
	}}
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	moved, intoEmpty := Suck(chest, hopper)
	if !moved || !intoEmpty || !hopper.slots[0].Equal(item(t, "minecraft:dirt", 1)) || !hopper.slots[1].Empty() ||
		chest.slots[0].Count != 1 || chest.slots[1].Count != 1 || chest.slots[2].Count != 1 {
		t.Fatalf("moved %v intoEmpty %v, hopper %v, chest %v", moved, intoEmpty, hopper.slots, chest.slots)
	}
}

func TestDispenseGiveBackIntoAFullDispenserReturnsTheStack(t *testing.T) {
	dispenser := &testBlock{kind: KindDispenser, slots: make([]protocol.ItemStack, 9)}
	for i := range dispenser.slots {
		dispenser.slots[i] = item(t, "minecraft:stone", 64)
	}
	bone := item(t, "minecraft:bone_meal", 1)
	if left := DispenseGiveBack(dispenser, 4, bone); !left.Equal(bone) {
		t.Fatalf("left %v, want the stack back", left)
	}
}

// BenchmarkSuckFromAnEmptyDoubleChest is an idle hopper's retry under an
// empty pair, which runs every tick and so must not allocate. The pair is
// handed over as its Block once, the way the hopper's target already is;
// the move reads it slot by slot.
func BenchmarkSuckFromAnEmptyDoubleChest(b *testing.B) {
	var chest Block = Compound{
		First:  &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 27)},
		Second: &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 27)},
	}
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	b.ReportAllocs()
	for b.Loop() {
		Suck(chest, hopper)
	}
}

// BenchmarkEjectIntoADoubleChest is a hopper pushing into a pair one stone
// short of full, so every check walks the whole pair. The state is put back
// each turn to keep it there, and the pair is handed over as its Block once,
// the way the hopper's target already is.
func BenchmarkEjectIntoADoubleChest(b *testing.B) {
	first := &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 27)}
	second := &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 27)}
	full := item(b, "minecraft:stone", 64)
	roomy := item(b, "minecraft:stone", 63)
	for i := range first.slots {
		first.slots[i] = full
	}
	for i := range second.slots {
		second.slots[i] = full
	}
	second.slots[26] = roomy // pair slot 53
	var chest Block = Compound{First: first, Second: second}
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	hopper.slots[0] = full
	b.ReportAllocs()
	for b.Loop() {
		Eject(hopper, chest, protocol.FaceBottom)
		chest.Put(53, roomy)
		hopper.slots[0] = full
	}
}

func TestBrewingStandBottleSlotsTakeOneBottleEach(t *testing.T) {
	stand := &testBlock{kind: KindBrewingStand, slots: make([]protocol.ItemStack, 5)}
	for i := range 5 {
		left, _ := AddItem(stand, item(t, "minecraft:glass_bottle", 1), protocol.FaceNorth, true)
		if wantLeft := i >= 3; left.Empty() == wantLeft {
			t.Fatalf("bottle %d: left %v", i, left)
		}
	}
	for i := range 3 {
		if stand.slots[i].Count != 1 {
			t.Errorf("slot %d holds %d bottles, want 1", i, stand.slots[i].Count)
		}
	}
}
