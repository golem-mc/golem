package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The double-click gather: mode 6 of a click, which tops the cursor's stack
// up from the same items wherever the window keeps them. These drive it over
// a real connection to a chest, the window with the most room to arrange the
// layout a test names.

// TestDoubleClickGatherFillsFromAPartialStackFirst arranges the layout the
// two-pass sweep exists for: a full stack ahead of a partial one. The gather
// fills from the partial stack first, so the full stack is only opened for
// the remainder - the vanilla collect's first pass skips the stacks already
// full and its second pass takes from them.
func TestDoubleClickGatherFillsFromAPartialStackFirst(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	stone := "minecraft:stone"
	// Chest slot 0 takes a whole stack, slot 1 a partial one.
	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 64))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 64))
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: servertest.ItemStack(t, stone, 64)}, protocol.ItemStack{})

	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 30))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 30))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: servertest.ItemStack(t, stone, 30)}, protocol.ItemStack{})

	// Ten stone on the cursor, then a double click on the empty slot 5: the
	// partial thirty goes in first, the full stack gives the last twenty-four.
	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 10))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 10))

	w.Click(5, 0, protocol.ClickModeCollect)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: protocol.ItemStack{},
		0: servertest.ItemStack(t, stone, 40),
	}, servertest.ItemStack(t, stone, 64))

	contents := srv.InventoryAt(4, 64, 0)
	if got := contents[0]; !got.Equal(servertest.ItemStack(t, stone, 40)) {
		t.Errorf("chest slot 0 = %+v, want the forty the full stack was left with", got)
	}
	if got := contents[1]; !got.Empty() {
		t.Errorf("chest slot 1 = %+v, want it drained into the cursor", got)
	}
}

// TestDoubleClickGatherOnAStackStillHoldingTheItemDoesNothing checks the
// clicked slot's own condition: the gather runs only when that slot is empty,
// so a double click on a slot that still holds the item leaves the window
// alone even though other slots hold the same item.
func TestDoubleClickGatherOnAStackStillHoldingTheItemDoesNothing(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	stone := "minecraft:stone"
	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 20))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 20))
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: servertest.ItemStack(t, stone, 20)}, protocol.ItemStack{})

	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 30))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 30))
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: servertest.ItemStack(t, stone, 30)}, protocol.ItemStack{})

	w.SetCreativeSlot(36, servertest.ItemStack(t, stone, 10))
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, servertest.ItemStack(t, stone, 10))

	// A double click on slot 0, which still holds its twenty stone: nothing
	// moves, the cursor keeps its ten and slot 1 keeps its thirty.
	w.Click(0, 0, protocol.ClickModeCollect)
	w.ExpectClickResult(nil, servertest.ItemStack(t, stone, 10))

	contents := srv.InventoryAt(4, 64, 0)
	if got := contents[0]; !got.Equal(servertest.ItemStack(t, stone, 20)) {
		t.Errorf("chest slot 0 = %+v, want the twenty left untouched", got)
	}
	if got := contents[1]; !got.Equal(servertest.ItemStack(t, stone, 30)) {
		t.Errorf("chest slot 1 = %+v, want the thirty left untouched", got)
	}
}
