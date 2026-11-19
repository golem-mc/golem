package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The cartography table's part of a click: Menu[0] the map, Menu[1] paper,
// an empty map or a glass pane, Menu[CartographyResultSlot] the stored
// result, recomputed here and by internal/server/containers.go's input hooks. A take
// spends one of each input, then the rest refill the result; emptying either
// input clears it.
// vanilla: CartographyTableMenu

// RecomputeCartography recomputes the result from the two inputs and the
// map's saved data as mapState (player.Inventory.MapState) reads it, storing
// it when it differs; stored reports which.
func RecomputeCartography(menu []protocol.ItemStack, mapState func(int32) (int8, bool, bool)) (stored bool) {
	result := cartographyResult(menu[0], menu[1], mapState)
	if result.Equal(menu[CartographyResultSlot]) {
		return false
	}
	menu[CartographyResultSlot] = result
	return true
}

// cartographyResult is what the inputs make: with paper a copy marked for
// zoom out (an #extendable_maps map below scale 4), with a glass pane one
// marked for locking, with an empty map the two maps a clone hands back. A
// locked map neither zooms nor locks; a map without saved data, anything
// else, or no map, shows nothing. A nil mapState offers every recipe.
// vanilla: CartographyTableMenu.setupResultSlot
func cartographyResult(mapStack, additional protocol.ItemStack, mapState func(int32) (int8, bool, bool)) protocol.ItemStack {
	mapID, ok := mapStack.MapIDOf()
	if mapStack.Empty() || additional.Empty() || !ok {
		return protocol.ItemStack{}
	}
	var scale int8
	var locked bool
	if mapState != nil {
		if scale, locked, ok = mapState(mapID); !ok {
			return protocol.ItemStack{}
		}
	}
	result := mapStack
	switch registry.Item(additional.ItemID) {
	case registry.ItemPaper:
		if !registry.Item(mapStack.ItemID).InTag(registry.ItemTagIDExtendableMaps) || locked || scale >= 4 {
			return protocol.ItemStack{}
		}
		result.Count = 1
		result.SetMapPostProcessing(protocol.MapPostProcessingScale)
	case registry.ItemGlassPane:
		if locked {
			return protocol.ItemStack{}
		}
		result.Count = 1
		result.SetMapPostProcessing(protocol.MapPostProcessingLock)
	case registry.ItemMap:
		result.Count = 2
	default:
		return protocol.ItemStack{}
	}
	return result
}

// isCartographyMap reports whether the stack carries a map id, whatever its
// item. vanilla: CartographyTableMenu's map slot's mayPlace
func isCartographyMap(stack protocol.ItemStack) bool {
	_, ok := stack.MapIDOf()
	return ok
}

// isCartographyAddition reports paper (zoom), an empty map (clone) or a
// glass pane (lock).
// vanilla: CartographyTableMenu's addition slot's mayPlace
func isCartographyAddition(stack protocol.ItemStack) bool {
	switch registry.Item(stack.ItemID) {
	case registry.ItemPaper, registry.ItemMap, registry.ItemGlassPane:
		return true
	}
	return false
}

// cartographyTake spends one of each input and recomputes; either input's
// last piece clears the result. The sound (at most once a tick in vanilla)
// is the outcome's flag for the caller.
// vanilla: CartographyTableMenu's result slot's onTake
func cartographyTake(view View, inv *player.Inventory, outcome *Outcome) {
	for slot := range int32(CartographyResultSlot) {
		input := view.Stack(inv, slot)
		if input.Empty() {
			continue
		}
		input.Count--
		if input.Count <= 0 {
			input = protocol.ItemStack{}
		}
		setStack(view, inv, slot, input, outcome)
	}
	if stored := RecomputeCartography(view.Menu, inv.MapState); stored {
		outcome.touched(CartographyResultSlot)
	}
	outcome.CartographyTaken = true
}

// cartographyResultSlot is the result slot's click handling; the take
// post-processes the map, each zoom or lock under its own fresh id.
// vanilla: MapItem.onCraftedBy
var cartographyResultSlot = resultSlot{slot: CartographyResultSlot, output: (*player.Inventory).TakeCraft, take: cartographyTake}
