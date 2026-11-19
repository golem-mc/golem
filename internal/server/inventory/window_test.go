package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestStonecutterPickAndInputChange checks the pick lays the recipe in the
// result slot once, and a new input item clears the selection and the
// result, announcing the result slot.
func TestStonecutterPickAndInputChange(t *testing.T) {
	stone := itemID(t, "minecraft:stone")
	var w Window
	w.Menu[0] = protocol.ItemStack{ItemID: stone, Count: 1}
	w.Recipe, w.SentRecipe, w.RecipeInput = -1, -1, stone

	got, ok := w.PickStonecutter(0)
	if !ok || got.Empty() || !w.Menu[StonecutterResultSlot].Equal(got) || w.Recipe != 0 {
		t.Fatalf("pick 0 = %+v, %t; window %+v", got, ok, w)
	}
	if _, ok := w.PickStonecutter(0); ok {
		t.Error("the same pick again took")
	}
	if selected, moved := w.StonecutterSelectionMoved(); !moved || selected != 0 {
		t.Errorf("selection moved = %d, %t, want 0, true", selected, moved)
	}
	if _, moved := w.StonecutterSelectionMoved(); moved {
		t.Error("the selection moved twice")
	}

	w.Menu[0] = protocol.ItemStack{}
	outcome := Outcome{Announce: []int32{0}}
	if w.SlotsChanged(KindStonecutter, nil, &outcome, false) {
		t.Error("a stonecutter asked for the enchanting offers")
	}
	if w.Recipe != -1 || !w.Menu[StonecutterResultSlot].Empty() ||
		!slices.Contains(outcome.Announce, StonecutterResultSlot) {
		t.Errorf("after the input went: recipe %d, result %+v, announce %v",
			w.Recipe, w.Menu[StonecutterResultSlot], outcome.Announce)
	}
}

// TestSlotsChangedAsksForTheEnchantOffers checks the enchanting table's
// item or lapis moving is handed back to the caller.
func TestSlotsChangedAsksForTheEnchantOffers(t *testing.T) {
	var w Window
	if !w.SlotsChanged(KindEnchant, nil, &Outcome{Announce: []int32{1}}, false) {
		t.Error("the lapis moving did not ask for the offers")
	}
	if w.SlotsChanged(KindEnchant, nil, &Outcome{Announce: []int32{5}}, false) {
		t.Error("a player slot moving asked for the offers")
	}
}

// TestCorrections checks an announced slot is always corrected, a matching
// claim is not, and a mismatching or incomparable one is.
func TestCorrections(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	held := map[int32]protocol.ItemStack{3: stone, 4: stone, 5: stone}
	read := func(slot int32) protocol.ItemStack { return held[slot] }
	claims := []SlotClaim{
		{Slot: 3, Claim: protocol.HashedSlot{Stack: stone}, Comparable: true},
		{Slot: 4, Claim: protocol.HashedSlot{}, Comparable: true},
		{Slot: 5, Claim: protocol.HashedSlot{Stack: stone}, Comparable: false},
		{Slot: 2, Claim: protocol.HashedSlot{Stack: stone}, Comparable: true},
	}
	got := Corrections(claims, []int32{2, 99}, 10, read)
	want := map[int32]bool{2: true, 4: true, 5: true}
	if len(got) != len(want) {
		t.Fatalf("corrections %v, want slots %v", got, want)
	}
	for slot := range want {
		if _, ok := got[slot]; !ok {
			t.Errorf("slot %d not corrected", slot)
		}
	}
	if claims[3].Slot != 2 {
		t.Error("the caller's claims were changed")
	}
}

// TestOpenWindowIDsWrap checks the window IDs count from 1 through 100, then
// start over.
func TestOpenWindowIDsWrap(t *testing.T) {
	var s Session
	if id := s.OpenWindow(); id != 1 {
		t.Fatalf("first window ID %d, want 1", id)
	}
	for range 98 {
		s.OpenWindow()
	}
	if id := s.OpenWindow(); id != 100 {
		t.Errorf("100th window ID = %d, want 100", id)
	}
	if id := s.OpenWindow(); id != 1 {
		t.Errorf("101st window ID = %d, want 1", id)
	}
}
