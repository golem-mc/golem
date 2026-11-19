package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestACrafterSlotHoldingAnItemCannotBeDisabled puts an item in grid slot 0
// and sends the disable toggle for it and for the empty slot 1: only slot 1
// ends up disabled.
// vanilla: CrafterBlockEntity.setSlotState
func TestACrafterSlotHoldingAnItemCannotBeDisabled(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openCrafter(t, w)

	stone := servertest.ItemStack(t, "minecraft:stone", 1)
	w.SetCreativeSlot(36, stone)
	w.Click(9+27, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: stone, 9 + 27: {}}, protocol.ItemStack{})

	toggleCrafterSlot(w, 0, false)
	toggleCrafterSlot(w, 1, false)
	w.Settle()

	disabled, ok := srv.CrafterDisabledSlots(4, 64, 0)
	if !ok {
		t.Fatal("the crafter's block entity went missing")
	}
	if disabled[0] {
		t.Error("slot 0 holds an item and was disabled")
	}
	if !disabled[1] {
		t.Error("empty slot 1 was not disabled")
	}
}

// TestAnItemCommandIntoADisabledCrafterSlotEnablesIt disables grid slot 0 and
// puts stone into it with /item: the write enables the slot and the open
// window is told.
// vanilla: CrafterBlockEntity.setItem
func TestAnItemCommandIntoADisabledCrafterSlotEnablesIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)
	openCrafter(t, w)
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)

	toggleCrafterSlot(w, 0, false)
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)

	w.ChatCommand("item replace block 4 64 0 container.0 with minecraft:stone")
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 0)

	disabled, ok := srv.CrafterDisabledSlots(4, 64, 0)
	if !ok {
		t.Fatal("the crafter's block entity went missing")
	}
	if disabled[0] {
		t.Error("slot 0 is still disabled after /item put stone in it")
	}
}
