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
