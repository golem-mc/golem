package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// Window is an open container window's own state: slots no block entity
// holds and its data numbers, each beside what the client was last told, so
// a send diffs (vanilla sends a data slot only when it moved). The caller
// holds the player's inventory lock throughout.
//
// A crafting table keeps its 3x3 in Grid; closing hands it back to the
// inventory or spills it.
//
// Menu holds the other windows' own slots: the enchanting table's item and
// lapis; the anvil's, stonecutter's, smithing table's, grindstone's and
// cartography table's inputs with their result's stored stack; the beacon's
// payment; the merchant's two payments and the offer.
type Window struct {
	Grid [CraftingGridCells]protocol.ItemStack
	Menu [4]protocol.ItemStack

	// Offers is the trades of the merchant this window is open on, shared
	// with the merchant, and Hint the offer the player last selected.
	Offers []Offer
	Hint   int32

	// Rename is the name typed into the anvil's field; Cost the level cost
	// its last recompute came to, SentCost the one the client was told.
	Rename   string
	SentCost int32
	Cost     int32

	// Stonecutter selection: the picked recipe index (-1 none), what the
	// client was last told, and the input item it was made for.
	Recipe      int32
	SentRecipe  int32
	RecipeInput int32

	// Loom selection: the LoomPatterns index picked (-1 none) and what the
	// client was last told. LoomPatterns is the pattern slot's
	// banner_pattern IDs in the client's list order.
	LoomSelected     int32
	SentLoomSelected int32
	LoomPatterns     []int32

	// SmithingError (property 0, the cross over the result) is whether the
	// inputs match no recipe; SentSmithingError what the client was told.
	SmithingError     int32
	SentSmithingError int32

	// Props are the enchanting offers drawn now from item, seed and
	// shelves; SentProps what the client was last told.
	Props     [protocol.EnchantPropertyCount]int32
	SentProps [protocol.EnchantPropertyCount]int32

	// SentBeacon is the beacon window's numbers the client was last told,
	// which the level's updates and the choices' diff against.
	SentBeacon [protocol.BeaconPropertyCount]int32

	// SentCrafter is the crafter window's numbers as last told: 0-8 the
	// disabled slots, 9 triggered.
	SentCrafter [protocol.CrafterPropertyTriggered + 1]int32

	// Sent is what this player was last told each window slot holds; a
	// broadcast resends only differing slots.
	// vanilla: AbstractContainerMenu.remoteSlots
	Sent []protocol.ItemStack
}

// SetSent records the stack the player was last told window slot holds; a
// slot outside Sent is ignored.
func (w *Window) SetSent(slot int32, stack protocol.ItemStack) {
	if slot < 0 || int(slot) >= len(w.Sent) {
		return
	}
	w.Sent[slot] = stack
}

// View is the window in click-logic terms with its own slots filled in; the
// caller adds container slots for kinds that have them.
func (w *Window) View(kind Kind, id int32) View {
	view := View{Kind: kind, ID: id}
	switch kind {
	case KindCrafting:
		view.Grid = w.Grid[:]
	case KindAnvil:
		view.Menu = w.Menu[:]
		view.AnvilName = w.Rename
	case KindStonecutter:
		view.Menu = w.Menu[:]
		if recipe, ok := stonecutterRecipe(w.Menu[0].ItemID, w.Recipe); ok {
			view.StonecutterRecipe = recipe.ID
		}
	case KindEnchant, KindBeacon, KindSmithing, KindGrindstone, KindCartography:
		view.Menu = w.Menu[:]
	case KindLoom:
		view.Menu = w.Menu[:]
		view.Loom = w
	case KindMerchant:
		view.Menu = w.Menu[:]
		view.Offers = w.Offers
		view.Hint = w.Hint
	}
	return view
}

// SlotsChanged recomputes the results of inputs the outcome announced after
// a click or drag, announcing the result slot where it changed. It reports
// whether the enchanting offers need a recompute, which reads the world's
// shelves and is the caller's. inv is the clicking player's, whose map
// hooks the cartography table reads.
// vanilla: AbstractContainerMenu.slotsChanged
func (w *Window) SlotsChanged(kind Kind, inv *player.Inventory, outcome *Outcome, creative bool) (enchant bool) {
	announced := func(slots ...int32) bool {
		for _, slot := range slots {
			if slices.Contains(outcome.Announce, slot) {
				return true
			}
		}
		return false
	}
	switch kind {
	case KindAnvil:
		if outcome.AnvilTaken || announced(0, 1) {
			// The take empties the inputs, so the recompute yields nothing, and
			// the cost rides along for the property sent after unlocking. An
			// unchanged result is not announced.
			var stored bool
			w.Cost, stored = RecomputeAnvil(w.Menu[:], w.Rename, creative, outcome.AnvilTaken)
			if stored {
				outcome.Announce = append(outcome.Announce, AnvilResultSlot)
			}
		}
	case KindStonecutter:
		if announced(0) {
			// A take's spending of the input's last piece lands here
			// too, and the result goes empty with it.
			w.stonecutterInputChanged(outcome)
		}
	case KindSmithing:
		if announced(0, 1, 2) {
			// A take's input spending lands here too; the take already made the
			// remaining result, so the recompute stores the same stack silently.
			w.smithingInputsChanged(outcome)
		}
	case KindEnchant:
		// vanilla: EnchantmentMenu.slotsChanged
		return announced(0, 1)
	case KindGrindstone:
		// Forced by the take, so its spent answer clears.
		if announced(0, 1) && RecomputeGrindstone(w.Menu[:], outcome.GrindstoneTaken) {
			outcome.Announce = append(outcome.Announce, GrindstoneResultSlot)
		}
	case KindCartography:
		if announced(0, 1) && RecomputeCartography(w.Menu[:], inv.MapState) {
			outcome.Announce = append(outcome.Announce, CartographyResultSlot)
		}
	case KindLoom:
		if announced(0, 1, 2) {
			w.loomInputsChanged(outcome)
		}
	case KindMerchant:
		if announced(0, 1) {
			outcome.MerchantUpdated = true
			if RecomputeMerchant(w.Menu[:], w.Offers, w.Hint) {
				outcome.Announce = append(outcome.Announce, MerchantResultSlot)
			}
		}
	}
	return false
}

// stonecutterInputChanged resets the selection and result when the input's
// item changed (to empty included): no selection outlives its item.
// vanilla: StonecutterMenu.setupRecipeList
func (w *Window) stonecutterInputChanged(outcome *Outcome) {
	item := w.Menu[0].ItemID
	if item == w.RecipeInput {
		return
	}
	w.RecipeInput = item
	w.Recipe = -1
	if !w.Menu[StonecutterResultSlot].Empty() {
		w.Menu[StonecutterResultSlot] = protocol.ItemStack{}
		outcome.Announce = append(outcome.Announce, StonecutterResultSlot)
	}
}

// smithingInputsChanged recomputes the result and error state from the
// announced inputs.
// vanilla: SmithingMenu.slotsChanged, SmithingMenu.createResult
func (w *Window) smithingInputsChanged(outcome *Outcome) {
	if RecomputeSmithing(w.Menu[:]) {
		outcome.Announce = append(outcome.Announce, SmithingResultSlot)
	}
	w.SmithingError = 0
	if !w.Menu[0].Empty() && !w.Menu[1].Empty() && !w.Menu[2].Empty() &&
		w.Menu[SmithingResultSlot].Empty() {
		w.SmithingError = 1
	}
}

// PickStonecutter selects an input recipe and puts its output in the result
// slot, returning it; a repeat pick or an out-of-range index reports false.
// vanilla: StonecutterMenu.clickMenuButton
func (w *Window) PickStonecutter(button int32) (protocol.ItemStack, bool) {
	if w.Recipe == button {
		return protocol.ItemStack{}, false
	}
	recipe, ok := StonecutterResult(w.Menu[0].ItemID, button)
	if !ok {
		return protocol.ItemStack{}, false
	}
	w.Recipe = button
	w.Menu[StonecutterResultSlot] = recipe
	return recipe, true
}

// SetRename stores the anvil's validated name and remakes the result and
// the cost for it.
func (w *Window) SetRename(name string, creative bool) {
	w.Rename = name
	w.Cost, _ = RecomputeAnvil(w.Menu[:], w.Rename, creative, true)
}

// AnvilCostMoved returns the anvil's cost and records it as told, reporting
// false when it is what the client was last told.
func (w *Window) AnvilCostMoved() (int32, bool) {
	return moved(w.Cost, &w.SentCost)
}

// StonecutterSelectionMoved is AnvilCostMoved for the stonecutter's
// selection.
func (w *Window) StonecutterSelectionMoved() (int32, bool) {
	return moved(w.Recipe, &w.SentRecipe)
}

// LoomSelectionMoved is AnvilCostMoved for the loom's selection.
func (w *Window) LoomSelectionMoved() (int32, bool) {
	return moved(w.LoomSelected, &w.SentLoomSelected)
}

// SmithingErrorMoved is AnvilCostMoved for the smithing table's error state.
func (w *Window) SmithingErrorMoved() (int32, bool) {
	return moved(w.SmithingError, &w.SentSmithingError)
}

// moved records value as sent, reporting whether it differed.
func moved(value int32, sent *int32) (int32, bool) {
	if value == *sent {
		return value, false
	}
	*sent = value
	return value, true
}

// SlotClaim is the client's belief about one changed slot: its number, the
// stack it thinks is there (damage taken from outside the checksum, since
// the hash cannot carry wear back), and whether the server can compare it.
// An uncomparable claim, such as a hashed component the server cannot check,
// counts as a disagreement and the slot is corrected.
type SlotClaim struct {
	Slot       int32
	Claim      protocol.HashedSlot
	Comparable bool
}

// Corrections is the slots a click is answered with: every announced slot in
// the window regardless of prediction, plus each claimed slot that does not
// match stack. The caller holds the locks stack reads under.
func Corrections(claims []SlotClaim, announce []int32, slots int32, stack func(int32) protocol.ItemStack) map[int32]protocol.ItemStack {
	claims = slices.Clone(claims)
	out := make(map[int32]protocol.ItemStack)
	for _, claimed := range claims {
		if claimed.Slot < 0 {
			continue
		}
		out[claimed.Slot] = stack(claimed.Slot)
	}
	for _, announced := range announce {
		if announced >= 0 && announced < slots {
			out[announced] = stack(announced)
		}
	}
	// The claims about announced slots are dropped: the correction covers
	// them.
	for i := range claims {
		if claims[i].Slot >= 0 && slices.Contains(announce, claims[i].Slot) {
			claims[i].Slot = -1
		}
	}
	for _, claimed := range claims {
		if claimed.Slot < 0 {
			continue
		}
		if claimed.Comparable && claimed.Claim.Matches(out[claimed.Slot]) {
			delete(out, claimed.Slot)
			continue
		}
		out[claimed.Slot] = stack(claimed.Slot)
	}
	return out
}
