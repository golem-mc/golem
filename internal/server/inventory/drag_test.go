package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestDragDividesTheCarriedStackOverTheMarks checks the paint drag's
// arithmetic: the left button splits the held stack evenly over the marked
// slots (the floor of the share each, the remainder on the cursor) and the
// right leaves one item in each; a slot painted twice is painted once, and
// a drag that never began has nothing to end.
func TestDragDividesTheCarriedStackOverTheMarks(t *testing.T) {
	sticks64 := protocol.ItemStack{ItemID: itemID(t, "minecraft:stick"), Count: 64}
	sticks32 := protocol.ItemStack{ItemID: sticks64.ItemID, Count: 32}

	inv := &player.Inventory{}
	chest := make(slots, 27)
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: sticks64}

	// Begin, paint two slots - the one slot painted twice painting once -
	// and end: sixty-four sticks over two slots is thirty-two each.
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 10)
	session.MarkDrag(view, inv, 10)
	session.MarkDrag(view, inv, 11)
	session.MarkDrag(view, inv, view.Slots()) // a slot the window does not number
	var outcome Outcome
	if !session.EndDrag(view, inv, false, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	if !chest[10].Equal(sticks32) || !chest[11].Equal(sticks32) {
		t.Errorf("the marked slots hold %+v and %+v, want thirty-two sticks each",
			chest[10], chest[11])
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v after an even split, want nothing left", session.Carried)
	}
	if len(outcome.Announce) != 2 || outcome.Announce[0] != 10 || outcome.Announce[1] != 11 {
		t.Errorf("announced %+v, want the two marked slots in the order painted", outcome.Announce)
	}
	if len(outcome.EntitySlots) != 2 {
		t.Errorf("the other viewers were not told of both slots: %+v", outcome.EntitySlots)
	}

	// The uneven share keeps the remainder on the cursor.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: sticks64}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	session.MarkDrag(view, inv, 2)
	outcome = Outcome{}
	session.EndDrag(view, inv, false, 0, &outcome)
	for _, slot := range []int{0, 1, 2} {
		if !chest[slot].Equal(protocol.ItemStack{ItemID: sticks64.ItemID, Count: 21}) {
			t.Errorf("slot %d = %+v, want twenty-one sticks (the floor of the share)", slot, chest[slot])
		}
	}
	if got := session.Carried; got.ItemID != sticks64.ItemID || got.Count != 1 {
		t.Errorf("carried = %+v, want the one stick the slots could not split", got)
	}

	// The right button leaves one item in each marked slot.
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 3}
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: stone}
	session.BeginDrag(4, false)
	session.MarkDrag(view, inv, 5)
	session.MarkDrag(view, inv, 6)
	session.MarkDrag(view, inv, 7)
	outcome = Outcome{}
	session.EndDrag(view, inv, false, 0, &outcome)
	for _, slot := range []int{5, 6, 7} {
		if !chest[slot].Equal(protocol.ItemStack{ItemID: stone.ItemID, Count: 1}) {
			t.Errorf("slot %d = %+v, want one stone (the right button's one each)", slot, chest[slot])
		}
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v after the one-each split", session.Carried)
	}

	// A drag that never began reports false on the end and changes nothing.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 10)
	outcome = Outcome{}
	if session.EndDrag(view, inv, false, 0, &outcome) {
		t.Error("a drag that never began was reported ended")
	}
	if len(outcome.Announce) != 0 || !session.Carried.Empty() {
		t.Errorf("the never-begun drag changed things: %+v", outcome)
	}

	// A cancelled drag ends the same way.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: sticks64}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 10)
	session.CancelDrag()
	outcome = Outcome{}
	if session.EndDrag(view, inv, false, 0, &outcome) {
		t.Error("a cancelled drag was nevertheless divided out")
	}
	if got := session.Carried; !got.Equal(sticks64) {
		t.Errorf("carried = %+v, want the stack untouched by the cancelled drag", got)
	}
}

// TestDragKeepsTheCursorComponents checks what a drag leaves on the cursor
// when the marks do not take the whole stack: the remainder is the carried
// stack with its count cut down, so its components stay on it - the marked
// slots and the cursor alike keep the enchantment and the name.
func TestDragKeepsTheCursorComponents(t *testing.T) {
	name := protocol.Text{Text: "Bob"}
	sword := protocol.ItemStack{
		ItemID:       itemID(t, "minecraft:diamond_sword"),
		Count:        3,
		Enchantments: map[int32]int32{1: 2},
		CustomName:   &name,
	}

	inv := &player.Inventory{}
	chest := make(slots, 27)
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: sword}

	session.BeginDrag(1, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	var outcome Outcome
	if !session.EndDrag(view, inv, false, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	one := sword
	one.Count = 1
	if !chest[0].Equal(one) || !chest[1].Equal(one) {
		t.Errorf("the marked slots hold %+v and %+v, want one sword with its components each",
			chest[0], chest[1])
	}
	if !session.Carried.Equal(one) {
		t.Errorf("carried = %+v, want the one sword left with its components", session.Carried)
	}
}

// TestDragMarksOnlySlotsItCanFill checks which slots a paint drag takes: a
// slot holding another stack is not marked, nor is one painted after the
// cursor's items run out, so the share divides over the marked slots alone
// rather than every slot the pointer crossed. A drag that marked a single
// slot runs as a plain pickup instead.
func TestDragMarksOnlySlotsItCanFill(t *testing.T) {
	inv := &player.Inventory{}
	stick := itemID(t, "minecraft:stick")
	stone5 := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 5}

	// Two sticks painted over three empty slots: the third is past the
	// items and is not marked, so the left drag leaves one in each of the
	// first two and nothing on the cursor.
	chest := make(slots, 27)
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 2}}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	session.MarkDrag(view, inv, 2)
	if len(session.dragSlots) != 2 {
		t.Fatalf("marked %v, want only the two slots the items reach", session.dragSlots)
	}
	var outcome Outcome
	if !session.EndDrag(view, inv, false, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	for _, slot := range []int{0, 1} {
		if !chest[slot].Equal(protocol.ItemStack{ItemID: stick, Count: 1}) {
			t.Errorf("slot %d = %+v, want one stick", slot, chest[slot])
		}
	}
	if !chest[2].Empty() {
		t.Errorf("slot 2 = %+v, want it left empty", chest[2])
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want nothing left", session.Carried)
	}

	// Ten sticks painted over an empty slot, a slot of stone and another
	// empty slot: the stone is not the sticks' kind and is not marked, so
	// the share is five each over the two empty slots.
	chest = make(slots, 27)
	chest[1] = stone5
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 10}}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	session.MarkDrag(view, inv, 2)
	if len(session.dragSlots) != 2 {
		t.Fatalf("marked %v, want the two empty slots only", session.dragSlots)
	}
	outcome = Outcome{}
	session.EndDrag(view, inv, false, 0, &outcome)
	five := protocol.ItemStack{ItemID: stick, Count: 5}
	if !chest[0].Equal(five) || !chest[2].Equal(five) {
		t.Errorf("the empty slots hold %+v and %+v, want five sticks each", chest[0], chest[2])
	}
	if !chest[1].Equal(stone5) {
		t.Errorf("the stone slot holds %+v, want it untouched", chest[1])
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want nothing left", session.Carried)
	}

	// The right button over more slots than items: the same two sticks
	// mark only two slots and leave one in each.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 2}}
	session.BeginDrag(4, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	session.MarkDrag(view, inv, 2)
	if len(session.dragSlots) != 2 {
		t.Fatalf("right-marked %v, want two slots", session.dragSlots)
	}
	outcome = Outcome{}
	session.EndDrag(view, inv, false, 0, &outcome)
	one := protocol.ItemStack{ItemID: stick, Count: 1}
	if !chest[0].Equal(one) || !chest[1].Equal(one) {
		t.Errorf("the right drag left %+v and %+v, want one stick each", chest[0], chest[1])
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want nothing after the right drag", session.Carried)
	}

	// A single mark is a plain pickup: the whole one-item stack goes into
	// the one slot the pointer reached.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 1}}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	if len(session.dragSlots) != 1 {
		t.Fatalf("marked %v, want the one slot the item reaches", session.dragSlots)
	}
	outcome = Outcome{}
	session.EndDrag(view, inv, false, 0, &outcome)
	if !chest[0].Equal(one) {
		t.Errorf("slot 0 = %+v, want the whole one stick", chest[0])
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want nothing after the single-slot pickup", session.Carried)
	}
}

// TestDragRechecksAMarkThatChangedBeforeTheEnd checks the end step against
// a marked slot whose contents changed after the paint - another viewer's
// click, a hopper: the slot no longer taking the cursor's stack is skipped,
// so it is not overwritten and no extra items are made.
func TestDragRechecksAMarkThatChangedBeforeTheEnd(t *testing.T) {
	inv := &player.Inventory{}
	stick := itemID(t, "minecraft:stick")
	stone5 := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 5}

	chest := make(slots, 27)
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 10}}
	session.BeginDrag(0, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	chest[1] = stone5
	var outcome Outcome
	if !session.EndDrag(view, inv, false, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	if want := (protocol.ItemStack{ItemID: stick, Count: 5}); !chest[0].Equal(want) {
		t.Errorf("slot 0 = %+v, want the floor share of five sticks", chest[0])
	}
	if !chest[1].Equal(stone5) {
		t.Errorf("slot 1 = %+v, want the stone left untouched", chest[1])
	}
	if got := session.Carried; got.ItemID != stick || got.Count != 5 {
		t.Errorf("carried = %+v, want the five sticks the skipped slot did not take", got)
	}
}

// TestDragRightButtonSkipsASlotAtItsLimit checks the right button against a
// marked slot already holding a full stack of the cursor's item: the mark
// takes nothing, so the full slot is left as it is and the mark past it
// still gets its one.
func TestDragRightButtonSkipsASlotAtItsLimit(t *testing.T) {
	inv := &player.Inventory{}
	stick := itemID(t, "minecraft:stick")
	full := protocol.ItemStack{ItemID: stick, Count: 64}

	chest := make(slots, 27)
	chest[0] = full
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: protocol.ItemStack{ItemID: stick, Count: 2}}
	session.BeginDrag(4, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	if len(session.dragSlots) != 2 {
		t.Fatalf("marked %v, want the full slot and the empty one", session.dragSlots)
	}
	var outcome Outcome
	if !session.EndDrag(view, inv, false, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	if !chest[0].Equal(full) {
		t.Errorf("slot 0 = %+v, want the full stack left alone", chest[0])
	}
	if want := (protocol.ItemStack{ItemID: stick, Count: 1}); !chest[1].Equal(want) {
		t.Errorf("slot 1 = %+v, want the one the right button leaves", chest[1])
	}
	if got := session.Carried; got.ItemID != stick || got.Count != 1 {
		t.Errorf("carried = %+v, want the one stick the full slot could not take", got)
	}
}

// TestDragMiddleButtonFillsEveryMarkInCreative checks the creative-only fill
// drag: each marked slot gets a full stack whatever the cursor holds, and
// the cursor, which the fills overdraw, ends empty. Outside creative the
// drag never begins, and a single mark does nothing.
func TestDragMiddleButtonFillsEveryMarkInCreative(t *testing.T) {
	inv := &player.Inventory{}
	stone := itemID(t, "minecraft:stone")

	chest := make(slots, 27)
	view := View{Kind: KindChest, ID: 1, Entity: chest}
	session := &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 1}}
	session.BeginDrag(8, true)
	for _, slot := range []int32{0, 1, 2} {
		session.MarkDrag(view, inv, slot)
	}
	var outcome Outcome
	if !session.EndDrag(view, inv, true, 0, &outcome) {
		t.Fatal("the drag was reported not to have ended")
	}
	for slot := range 3 {
		if want := (protocol.ItemStack{ItemID: stone, Count: 64}); !chest[slot].Equal(want) {
			t.Errorf("slot %d = %+v, want a full stack", slot, chest[slot])
		}
	}
	if !session.Carried.Empty() {
		t.Errorf("carried = %+v, want the overdrawn cursor empty", session.Carried)
	}

	// Survival: the drag does not begin, so nothing is marked or laid.
	chest = make(slots, 27)
	view = View{Kind: KindChest, ID: 1, Entity: chest}
	session = &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 1}}
	session.BeginDrag(8, false)
	session.MarkDrag(view, inv, 0)
	session.MarkDrag(view, inv, 1)
	if session.EndDrag(view, inv, false, 0, &outcome) || !chest[0].Empty() {
		t.Errorf("a survival middle drag began: slot 0 = %+v", chest[0])
	}

	// One mark: the fill type has no pickup button to re-run.
	session = &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 1}}
	session.BeginDrag(8, true)
	session.MarkDrag(view, inv, 5)
	session.EndDrag(view, inv, true, 0, &outcome)
	if !chest[5].Empty() || session.Carried.Count != 1 {
		t.Errorf("a single-mark fill moved something: slot 5 = %+v, carried = %+v",
			chest[5], session.Carried)
	}
}
