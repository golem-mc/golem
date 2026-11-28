package inventory

import (
	"slices"
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
func (b *testBlock) StackPtr(i int) *protocol.ItemStack {
	if i < 0 || i >= len(b.slots) {
		return nil
	}
	return &b.slots[i]
}
func (b *testBlock) Filled() uint64 {
	var m uint64
	for i, s := range b.slots {
		if s.Count > 0 {
			m |= 1 << i
		}
	}
	return m
}
func (b *testBlock) AdjustCount(i int, delta int32) { b.slots[i].Count += delta }
func (b *testBlock) Len() int                       { return len(b.slots) }
func (b *testBlock) Kind() Kind                     { return b.kind }

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

	custom := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	custom.slots[0] = item(t, "minecraft:stone", 3)
	custom.slots[0].SetMaxStackSize(4)
	in := item(t, "minecraft:stone", 3)
	in.SetMaxStackSize(4)
	left, intoEmpty = AddItem(custom, in, 0, false)
	if left.Count != 0 || intoEmpty || custom.slots[0].Count != 4 || custom.slots[1].Count != 2 {
		t.Fatalf("custom maximum: left %v, intoEmpty %v, slots %v", left, intoEmpty, custom.slots[:2])
	}

	worn := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	worn.slots[0] = item(t, "minecraft:stone", 5)
	other := item(t, "minecraft:stone", 1)
	other.Damage = 1
	left, _ = AddItem(worn, other, 0, false)
	if worn.slots[0].Count != 5 || worn.slots[1].Count != 1 || !left.Empty() {
		t.Fatalf("different components merged: left %v, slots %v", left, worn.slots[:2])
	}
}

func BenchmarkAddOneToOccupiedHopper(b *testing.B) {
	hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
	one, full := item(b, "minecraft:stone", 1), item(b, "minecraft:stone", 32)
	b.ReportAllocs()
	for b.Loop() {
		hopper.slots[0] = full
		left, intoEmpty := AddItem(hopper, one, protocol.FaceTop, false)
		if !left.Empty() || intoEmpty || hopper.slots[0].Count != 33 {
			b.Fatalf("left %v, intoEmpty %v, slot %v", left, intoEmpty, hopper.slots[0])
		}
	}
}

func TestHopperEjectIntoCrafter(t *testing.T) {
	stone := item(t, "minecraft:stone", 2)
	named := stone
	named.CustomName = &protocol.Text{Text: "Polished"}
	capped := stone
	capped.SetMaxStackSize(2)
	dirt := item(t, "minecraft:dirt", 32)
	for _, tc := range []struct {
		name      string
		incoming  protocol.ItemStack
		fill      protocol.ItemStack
		slots     map[int]protocol.ItemStack
		disabled  map[int]bool
		wantSlot  int
		intoEmpty bool
	}{
		{name: "skip disabled empty slot", incoming: stone, disabled: map[int]bool{0: true}, wantSlot: 1, intoEmpty: true},
		{name: "merge matching components", incoming: named, fill: dirt, slots: map[int]protocol.ItemStack{0: stone, 8: named}, wantSlot: 8},
		{name: "reject different components", incoming: named, fill: dirt, slots: map[int]protocol.ItemStack{8: stone}, wantSlot: -1},
		{name: "reject at custom stack limit", incoming: capped, fill: dirt, slots: map[int]protocol.ItemStack{8: capped}, wantSlot: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hopper := &testBlock{kind: KindHopper, slots: []protocol.ItemStack{tc.incoming}}
			crafter := &disabledBlock{
				kind: KindCrafter, slots: make([]protocol.ItemStack, 9),
				off: tc.disabled,
			}
			for i := range crafter.slots {
				crafter.Put(i, tc.fill)
			}
			for i, stack := range tc.slots {
				crafter.Put(i, stack)
			}
			want := slices.Clone(crafter.slots)
			wantSourceCount := tc.incoming.Count
			wantMoved := tc.wantSlot >= 0
			if wantMoved {
				count := want[tc.wantSlot].Count + 1
				want[tc.wantSlot] = tc.incoming
				want[tc.wantSlot].Count = count
				wantSourceCount--
			}

			moved, intoEmpty := Eject(hopper, crafter, protocol.FaceTop)
			if moved != wantMoved || intoEmpty != tc.intoEmpty {
				t.Errorf("Eject = (%v, %v), want (%v, %v)", moved, intoEmpty, wantMoved, tc.intoEmpty)
			}
			if got := hopper.Slot(0); got.Count != wantSourceCount || !got.Same(tc.incoming) {
				t.Errorf("hopper slot = %v, want %d of %v", got, wantSourceCount, tc.incoming)
			}
			for i, stack := range want {
				if got := crafter.Slot(i); got.Count != stack.Count || !got.Same(stack) {
					t.Errorf("crafter slot %d = %v, want %v", i, got, stack)
				}
			}
		})
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
	if canTakeThroughFace(furnace, &furnace.slots[1], 1, protocol.FaceBottom) {
		t.Error("the furnace's bottom gave up its fuel")
	}

	box := &testBlock{kind: KindShulker, slots: make([]protocol.ItemStack, 27)}
	if left, _ := AddItem(box, item(t, "minecraft:red_shulker_box", 1), protocol.FaceTop, true); left.Empty() {
		t.Error("a shulker box took a shulker box")
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

func TestFullDoubleChestDoesNotAllocate(t *testing.T) {
	full := item(t, "minecraft:stone", 64)
	chest := Compound{
		First:  &testBlock{kind: KindChest, slots: slices.Repeat([]protocol.ItemStack{full}, 27)},
		Second: &testBlock{kind: KindChest, slots: slices.Repeat([]protocol.ItemStack{full}, 27)},
	}
	if allocations := testing.AllocsPerRun(5, func() {
		if !Full(chest, protocol.FaceTop) {
			t.Fatal("the double chest is full")
		}
	}); allocations != 0 {
		t.Errorf("checking a full double chest allocated %g times, want 0", allocations)
	}
}

func TestEmptyAndFullMaskFastPaths(t *testing.T) {
	t.Parallel()
	chest := &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 27)}
	if !Empty(chest) || Full(chest, protocol.FaceTop) {
		t.Fatal("empty chest reported wrong Empty/Full")
	}
	chest.slots[26] = item(t, "minecraft:stone", 64)
	if Empty(chest) || Full(chest, protocol.FaceTop) {
		t.Fatal("single-slot chest reported wrong Empty/Full")
	}
	for i := range chest.slots {
		chest.slots[i] = item(t, "minecraft:stone", 64)
	}
	if Empty(chest) || !Full(chest, protocol.FaceTop) {
		t.Fatal("full chest reported wrong Empty/Full")
	}
	chest.slots[13].Count = 63
	if Full(chest, protocol.FaceTop) {
		t.Fatal("nearly full chest reported Full")
	}

	// Blocks with >64 slots fall back to scanning past the 64-bit mask.
	large := &testBlock{kind: KindChest, slots: make([]protocol.ItemStack, 70)}
	if !Empty(large) {
		t.Fatal("empty >64-slot block reported non-empty")
	}
	large.slots[69] = item(t, "minecraft:stone", 1)
	if Empty(large) {
		t.Fatal(">64-slot block with item in slot 69 reported empty")
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

type countingGate struct{ calls int }

func (g *countingGate) Check(one protocol.ItemStack) (protocol.ItemStack, bool) {
	g.calls++
	return one, true
}

func TestEjectFromTheOnlyOccupiedSlotIntoAFullPair(t *testing.T) {
	t.Parallel()
	build := func() (*testBlock, *testBlock, Block, *testBlock) {
		full := item(t, "minecraft:stone", 64)
		first := &testBlock{kind: KindChest, slots: slices.Repeat([]protocol.ItemStack{full}, 27)}
		second := &testBlock{kind: KindChest, slots: slices.Repeat([]protocol.ItemStack{full}, 27)}
		hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
		hopper.slots[2] = full
		return first, second, Compound{First: first, Second: second}, hopper
	}
	t.Run("no room", func(t *testing.T) {
		first, second, chest, hopper := build()
		moved, intoEmpty := Eject(hopper, chest, protocol.FaceBottom)
		if moved || intoEmpty || hopper.slots[2].Count != 64 || first.slots[0].Count != 64 || second.slots[26].Count != 64 {
			t.Fatalf("moved=%v intoEmpty=%v hopper=%d", moved, intoEmpty, hopper.slots[2].Count)
		}
	})
	t.Run("component capacity in the last slot", func(t *testing.T) {
		_, second, chest, hopper := build()
		second.slots[26].SetMaxStackSize(65)
		hopper.slots[2].SetMaxStackSize(65)
		moved, intoEmpty := Eject(hopper, chest, protocol.FaceBottom)
		if !moved || intoEmpty || hopper.slots[2].Count != 63 || second.slots[26].Count != 65 {
			t.Fatalf("moved=%v intoEmpty=%v hopper=%d last=%d", moved, intoEmpty, hopper.slots[2].Count, second.slots[26].Count)
		}
	})
	t.Run("gate not asked about a full destination", func(t *testing.T) {
		_, _, chest, hopper := build()
		gate := &countingGate{}
		if moved, _ := EjectGated(hopper, chest, protocol.FaceBottom, gate); moved || gate.calls != 0 {
			t.Fatalf("moved=%v gate calls=%d", moved, gate.calls)
		}
	})
}
