package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func loomWindow(t *testing.T, banner, dye, pattern string) *Window {
	t.Helper()
	var w Window
	w.LoomSelected = -1
	w.Menu[0] = protocol.ItemStack{ItemID: itemID(t, banner), Count: 1}
	w.Menu[1] = protocol.ItemStack{ItemID: itemID(t, dye), Count: 1}
	if pattern != "" {
		w.Menu[2] = protocol.ItemStack{ItemID: itemID(t, pattern), Count: 1}
	}
	return &w
}

// TestLoomPatternItemOffersItsPattern checks a pattern item replaces the
// open set with its one pattern, selected and shown at once.
func TestLoomPatternItemOffersItsPattern(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	outcome := Outcome{Announce: []int32{2}}
	w.SlotsChanged(KindLoom, nil, &outcome, false)
	creeper := registry.MustID("minecraft:banner_pattern", "minecraft:creeper")
	layers, ok := w.Menu[LoomResultSlot].BannerPatternsOf()
	if w.LoomSelected != 0 || !ok || len(layers) != 1 || layers[0].Pattern != creeper || layers[0].Dye != 15 {
		t.Errorf("selected %d, layers %+v", w.LoomSelected, layers)
	}
}

// TestLoomOffersNothingOnASixthLayer checks a banner at the layer limit
// shows no result and drops the selection.
func TestLoomOffersNothingOnASixthLayer(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	layer := protocol.BannerPatternLayer{Pattern: 0, Dye: 1}
	w.Menu[0].SetBannerPatterns([]protocol.BannerPatternLayer{layer, layer, layer, layer, layer})
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	if w.Menu[LoomResultSlot].Empty() {
		t.Fatal("five layers should still take a sixth")
	}
	w.Menu[0].SetBannerPatterns([]protocol.BannerPatternLayer{layer, layer, layer, layer, layer, layer})
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	if !w.Menu[LoomResultSlot].Empty() || w.LoomSelected != -1 {
		t.Errorf("six layers: result %+v, selected %d", w.Menu[LoomResultSlot], w.LoomSelected)
	}
}

// TestLoomSlotsTakeTheirOwnKinds checks the slot rules.
func TestLoomSlotsTakeTheirOwnKinds(t *testing.T) {
	view := View{Kind: KindLoom, Menu: make([]protocol.ItemStack, 4)}
	banner := protocol.ItemStack{ItemID: itemID(t, "minecraft:red_banner"), Count: 1}
	dye := protocol.ItemStack{ItemID: itemID(t, "minecraft:red_dye"), Count: 1}
	flower := protocol.ItemStack{ItemID: itemID(t, "minecraft:flower_banner_pattern"), Count: 1}
	for slot, tc := range []struct{ yes, no protocol.ItemStack }{{banner, dye}, {dye, flower}, {flower, banner}} {
		if !view.placeable(int32(slot), tc.yes) || view.placeable(int32(slot), tc.no) {
			t.Errorf("slot %d: takes %t, refuses %t", slot, view.placeable(int32(slot), tc.yes), !view.placeable(int32(slot), tc.no))
		}
	}
	if view.placeable(LoomResultSlot, banner) {
		t.Error("the result took a banner")
	}
}

// loomCapWindow is a loom over a stack of six-layer banners, a stack of
// black dye and a creeper pattern, with the pattern picked: the button does
// not check the cap, so the result shows a seventh layer.
func loomCapWindow(t *testing.T, count int32) *Window {
	t.Helper()
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	layer := protocol.BannerPatternLayer{Pattern: 0, Dye: 1}
	w.Menu[0].Count = count
	w.Menu[0].SetBannerPatterns([]protocol.BannerPatternLayer{layer, layer, layer, layer, layer, layer})
	w.Menu[1].Count = count
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0, 1, 2}}, false)
	if _, _, ok := w.PickLoom(0); !ok || w.Menu[LoomResultSlot].Empty() {
		t.Fatal("the pick should lay a seventh layer in the result")
	}
	return w
}

func countBanners(inv *player.Inventory) (n int32) {
	for _, slot := range inv.Slots {
		if registry.IsBannerItem(slot.ItemID) {
			n += slot.Count
		}
	}
	return n
}

// TestLoomShiftClickStopsAtTheLayerCap checks a take from a banner at the
// layer cap empties the result, so a shift-click makes one banner.
func TestLoomShiftClickStopsAtTheLayerCap(t *testing.T) {
	w := loomCapWindow(t, 5)
	inv := &player.Inventory{}
	var outcome Outcome
	(&Session{}).Click(w.View(KindLoom, 1), inv, protocol.ClickModeQuickMove, 0, LoomResultSlot, false, 0, &outcome)
	if got := countBanners(inv); got != 1 {
		t.Errorf("shift-click made %d banners, want 1", got)
	}
	if w.Menu[0].Count != 4 || w.Menu[1].Count != 4 || !w.Menu[LoomResultSlot].Empty() {
		t.Errorf("banner %d, dye %d, result %+v", w.Menu[0].Count, w.Menu[1].Count, w.Menu[LoomResultSlot])
	}
	if !outcome.LoomTaken {
		t.Error("the take was not marked for its sound")
	}
}

// TestLoomCtrlQStopsAtTheLayerCap checks Ctrl+Q throws one banner.
func TestLoomCtrlQStopsAtTheLayerCap(t *testing.T) {
	w := loomCapWindow(t, 5)
	var outcome Outcome
	(&Session{}).Click(w.View(KindLoom, 1), &player.Inventory{}, protocol.ClickModeThrow, 1, LoomResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || w.Menu[0].Count != 4 {
		t.Errorf("dropped %d, banner %d, want 1 and 4", len(outcome.Dropped), w.Menu[0].Count)
	}
}

// TestLoomPlainQThrowsTheResult checks Q throws the result and spends a
// banner and a dye.
func TestLoomPlainQThrowsTheResult(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	w.Menu[0].Count, w.Menu[1].Count = 3, 3
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	var outcome Outcome
	(&Session{}).Click(w.View(KindLoom, 1), &player.Inventory{}, protocol.ClickModeThrow, 0, LoomResultSlot, false, 0, &outcome)
	if len(outcome.Dropped) != 1 || w.Menu[0].Count != 2 || w.Menu[1].Count != 2 {
		t.Errorf("dropped %d, banner %d, dye %d", len(outcome.Dropped), w.Menu[0].Count, w.Menu[1].Count)
	}
}

// TestLoomNumberKeyTakesTheResult checks the swap into an empty hotbar slot
// and its refusal over a taken one.
func TestLoomNumberKeyTakesTheResult(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	w.Menu[0].Count, w.Menu[1].Count = 2, 2
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	want := w.Menu[LoomResultSlot]
	inv := &player.Inventory{}
	inv.Set(player.HotbarSlot+1, protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1})
	var outcome Outcome
	(&Session{}).Click(w.View(KindLoom, 1), inv, protocol.ClickModeSwap, 1, LoomResultSlot, false, 0, &outcome)
	if w.Menu[0].Count != 2 {
		t.Error("a swap over a taken slot spent the inputs")
	}
	(&Session{}).Click(w.View(KindLoom, 1), inv, protocol.ClickModeSwap, 0, LoomResultSlot, false, 0, &outcome)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(want) || w.Menu[0].Count != 1 || w.Menu[1].Count != 1 {
		t.Errorf("hotbar %+v, banner %d, dye %d", got, w.Menu[0].Count, w.Menu[1].Count)
	}
}

// TestClosingTheLoomHandsBackItsInputs checks the three inputs go back and
// the result goes with the window.
func TestClosingTheLoomHandsBackItsInputs(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "minecraft:creeper_banner_pattern")
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	inv := &player.Inventory{}
	(&Session{}).Close(w.View(KindLoom, 1), inv, false, false)
	var banners, dyes, patterns int
	for _, slot := range inv.Slots {
		switch {
		case registry.IsBannerItem(slot.ItemID):
			banners++
		case isLoomDye(slot):
			dyes++
		case isLoomPatternItem(slot):
			patterns++
		}
	}
	if banners != 1 || dyes != 1 || patterns != 1 {
		t.Errorf("handed back %d banners, %d dyes, %d patterns, want one each", banners, dyes, patterns)
	}
}

// TestLoomSelectionSurvivesADyeChange checks a picked pattern stays picked
// when the dye is swapped, and is dropped when the pattern item changes the
// offered set to one that lacks it.
func TestLoomSelectionSurvivesADyeChange(t *testing.T) {
	w := loomWindow(t, "minecraft:white_banner", "minecraft:black_dye", "")
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{0}}, false)
	if len(w.LoomPatterns) < 2 {
		t.Fatalf("the open set offers %d patterns", len(w.LoomPatterns))
	}
	if _, _, ok := w.PickLoom(1); !ok {
		t.Fatal("pick refused")
	}
	w.Menu[1] = protocol.ItemStack{ItemID: itemID(t, "minecraft:red_dye"), Count: 1}
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{1}}, false)
	layers, _ := w.Menu[LoomResultSlot].BannerPatternsOf()
	if w.LoomSelected != 1 || len(layers) != 1 || layers[0].Dye != 14 {
		t.Errorf("selected %d, layers %+v", w.LoomSelected, layers)
	}
	w.Menu[2] = protocol.ItemStack{ItemID: itemID(t, "minecraft:creeper_banner_pattern"), Count: 1}
	w.SlotsChanged(KindLoom, nil, &Outcome{Announce: []int32{2}}, false)
	if w.LoomSelected != 0 || len(w.LoomPatterns) != 1 {
		t.Errorf("selected %d of %d with a pattern item", w.LoomSelected, len(w.LoomPatterns))
	}
}
