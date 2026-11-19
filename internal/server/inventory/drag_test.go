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
