package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The player's own window's armour slots hold a single item: a whole stack
// left on one keeps the rest, which is what ArmorSlot.getMaxStackSize
// decides. A carved pumpkin is the stackable piece the issue names - it has
// the head slot's equippable component though a right-click places nothing.

// TestArmourSlotPlacesOneFromAStackOnTheCursor checks the plain pickup click
// on the head slot with a whole stack on the cursor: one item rests there and
// the cursor keeps the rest. A plain slot would take the whole stack.
// vanilla: ArmorSlot.getMaxStackSize
func TestArmourSlotPlacesOneFromAStackOnTheCursor(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pumpkins := servertest.ItemStack(t, "minecraft:carved_pumpkin", 64)
	w.SetCreativeSlot(player.MainSlot, pumpkins)
	w.Click(player.MainSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{player.MainSlot: {}}, pumpkins)

	w.Click(player.ArmorSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		player.ArmorSlot: servertest.ItemStack(t, "minecraft:carved_pumpkin", 1),
	}, servertest.ItemStack(t, "minecraft:carved_pumpkin", 63))

	if got := srv.InventorySlot("Testificate", player.ArmorSlot); !got.Equal(servertest.ItemStack(t, "minecraft:carved_pumpkin", 1)) {
		t.Errorf("the head slot = %+v, want the single pumpkin", got)
	}
}

// TestNumberKeySwapCapsTheHeadSlot checks the number key pressed on an empty
// head slot with a stack on the hotbar key it names: the head takes one and
// the key keeps the rest, the same split the vanilla swap makes.
// vanilla: AbstractContainerMenu.doClick SWAP
func TestNumberKeySwapCapsTheHeadSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pumpkins := servertest.ItemStack(t, "minecraft:carved_pumpkin", 64)
	w.SetCreativeSlot(player.HotbarSlot, pumpkins)

	w.Click(player.ArmorSlot, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		player.ArmorSlot:  servertest.ItemStack(t, "minecraft:carved_pumpkin", 1),
		player.HotbarSlot: servertest.ItemStack(t, "minecraft:carved_pumpkin", 63),
	}, protocol.ItemStack{})

	if got := srv.InventorySlot("Testificate", player.ArmorSlot); !got.Equal(servertest.ItemStack(t, "minecraft:carved_pumpkin", 1)) {
		t.Errorf("the head slot = %+v, want the single pumpkin", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(servertest.ItemStack(t, "minecraft:carved_pumpkin", 63)) {
		t.Errorf("the hotbar key = %+v, want the sixty-three left", got)
	}
}

// TestShiftClickPutsOnePumpkinOnTheHeadSlot checks the shift-click out of the
// player's things: the head slot takes a single pumpkin and the rest of the
// stack moves on among the player's things.
// vanilla: InventoryMenu.quickMoveStack
func TestShiftClickPutsOnePumpkinOnTheHeadSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pumpkins := servertest.ItemStack(t, "minecraft:carved_pumpkin", 64)
	w.SetCreativeSlot(player.MainSlot, pumpkins)
	w.Click(player.MainSlot, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		player.ArmorSlot:  servertest.ItemStack(t, "minecraft:carved_pumpkin", 1),
		player.HotbarSlot: servertest.ItemStack(t, "minecraft:carved_pumpkin", 63),
		player.MainSlot:   {},
	}, protocol.ItemStack{})

	if got := srv.InventorySlot("Testificate", player.ArmorSlot); !got.Equal(servertest.ItemStack(t, "minecraft:carved_pumpkin", 1)) {
		t.Errorf("the head slot = %+v, want the single pumpkin", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(servertest.ItemStack(t, "minecraft:carved_pumpkin", 63)) {
		t.Errorf("the first hotbar slot = %+v, want the sixty-three left", got)
	}
}
