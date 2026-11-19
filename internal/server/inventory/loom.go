package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The loom's part of a click: Menu[0] the banner, Menu[1] the dye, Menu[2]
// the pattern item, Menu[LoomResultSlot] the stored result, recomputed by
// Window.loomInputsChanged and pattern picks. A take spends one banner and
// one dye.
// vanilla: LoomMenu

// loomMaxLayers is the banner patterns a banner carries before the loom
// offers no more. vanilla: LoomMenu.slotsChanged
const loomMaxLayers = 6

// loomNoItemPatterns is the tag the pattern slot offers while it is empty.
// vanilla: BannerPatternTags.NO_ITEM_REQUIRED
const loomNoItemPatterns = "minecraft:no_item_required"

func isLoomBanner(stack protocol.ItemStack) bool { return registry.IsBannerItem(stack.ItemID) }

func isLoomDye(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID).InTag(registry.ItemTagIDLoomDyes)
}

func isLoomPatternItem(stack protocol.ItemStack) bool {
	if !registry.Item(stack.ItemID).InTag(registry.ItemTagIDLoomPatterns) {
		return false
	}
	_, ok := registry.StackBannerPatterns(stack)
	return ok
}

// loomPatternsFor is the patterns the pattern slot offers: the unlocked set
// while it is empty, otherwise what its item provides.
// vanilla: LoomMenu.getSelectablePatterns
func loomPatternsFor(pattern protocol.ItemStack) []int32 {
	if pattern.Empty() {
		return registry.BannerPatternTagIDs(loomNoItemPatterns)
	}
	ids, _ := registry.StackBannerPatterns(pattern)
	return ids
}

// loomResult is the banner with one more layer of the pattern in the dye's
// colour, empty when an input is missing.
// vanilla: LoomMenu.setupResultSlot
func loomResult(menu []protocol.ItemStack, pattern int32) protocol.ItemStack {
	banner, dye := menu[0], menu[1]
	if banner.Empty() || dye.Empty() {
		return protocol.ItemStack{}
	}
	colour, ok := registry.StackDyeColour(dye)
	if !ok {
		return protocol.ItemStack{}
	}
	layers, _ := banner.BannerPatternsOf()
	layers = append(slices.Clone(layers), protocol.BannerPatternLayer{Pattern: pattern, Dye: colour})
	result := banner
	result.Count = 1
	result.SetBannerPatterns(layers)
	return result
}

// setLoomResult stores the result when it differs, announcing the slot.
func (w *Window) setLoomResult(result protocol.ItemStack, outcome *Outcome) {
	if result.Equal(w.Menu[LoomResultSlot]) {
		return
	}
	w.Menu[LoomResultSlot] = result
	if outcome != nil {
		outcome.Announce = append(outcome.Announce, LoomResultSlot)
	}
}

// loomInputsChanged remakes the selectable patterns, the selection and the
// result from the three inputs.
// vanilla: LoomMenu.slotsChanged
func (w *Window) loomInputsChanged(outcome *Outcome) {
	if w.Menu[0].Empty() || w.Menu[1].Empty() {
		w.setLoomResult(protocol.ItemStack{}, outcome)
		w.LoomPatterns = nil
		w.LoomSelected = -1
		return
	}
	previous := w.LoomPatterns
	valid := w.LoomSelected >= 0 && int(w.LoomSelected) < len(previous)
	w.LoomPatterns = loomPatternsFor(w.Menu[2])
	display := int32(-1)
	switch {
	case len(w.LoomPatterns) == 1:
		w.LoomSelected = 0
		display = w.LoomPatterns[0]
	case !valid:
		w.LoomSelected = -1
	default:
		kept := slices.Index(w.LoomPatterns, previous[w.LoomSelected])
		w.LoomSelected = int32(kept)
		if kept >= 0 {
			display = w.LoomPatterns[kept]
		}
	}
	if display < 0 {
		w.setLoomResult(protocol.ItemStack{}, outcome)
		return
	}
	if layers, _ := w.Menu[0].BannerPatternsOf(); len(layers) >= loomMaxLayers {
		w.LoomSelected = -1
		w.setLoomResult(protocol.ItemStack{}, outcome)
		return
	}
	w.setLoomResult(loomResult(w.Menu[:], display), outcome)
}

// PickLoom selects a pattern and puts the patterned banner in the result.
// changed reports whether the result differs from what the client saw; ok
// is false for an index outside the offered patterns. The layer count is not
// checked here.
// vanilla: LoomMenu.clickMenuButton
func (w *Window) PickLoom(button int32) (result protocol.ItemStack, changed, ok bool) {
	if button < 0 || int(button) >= len(w.LoomPatterns) {
		return protocol.ItemStack{}, false, false
	}
	w.LoomSelected = button
	result = loomResult(w.Menu[:], w.LoomPatterns[button])
	changed = !result.Equal(w.Menu[LoomResultSlot])
	w.Menu[LoomResultSlot] = result
	return result, changed, true
}

// loomTake spends a banner and a dye, then recomputes, so a banner at the
// layer cap offers no further take.
// vanilla: LoomMenu's result slot's onTake, LoomMenu.slotsChanged
func loomTake(view View, inv *player.Inventory, outcome *Outcome) {
	for slot := range int32(2) {
		input := view.Menu[slot]
		if input.Empty() {
			continue
		}
		input.Count--
		if input.Count <= 0 {
			input = protocol.ItemStack{}
		}
		setStack(view, inv, slot, input, outcome)
	}
	switch {
	case view.Loom != nil:
		view.Loom.loomInputsChanged(outcome)
	case view.Menu[0].Empty() || view.Menu[1].Empty():
		view.Menu[LoomResultSlot] = protocol.ItemStack{}
		outcome.touched(LoomResultSlot)
	}
	outcome.LoomTaken = true
}

// loomResultSlot is the result slot's click handling.
var loomResultSlot = resultSlot{slot: LoomResultSlot, output: identityStack, take: loomTake}

func identityStack(_ *player.Inventory, stack protocol.ItemStack) protocol.ItemStack { return stack }
