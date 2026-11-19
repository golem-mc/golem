package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

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

// TestArmourSlotRightClickPlacesOne checks the right-click place-one path on
// the head slot: it places a single item and leaves the rest on the cursor.
func TestArmourSlotRightClickPlacesOne(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pumpkins := servertest.ItemStack(t, "minecraft:carved_pumpkin", 64)
	w.SetCreativeSlot(player.MainSlot, pumpkins)
	w.Click(player.MainSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{player.MainSlot: {}}, pumpkins)

	w.Click(player.ArmorSlot, 1, protocol.ClickModePickup)
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

// TestNumberKeySwapWithAWornPieceCapsTheHeadSlot checks the same key on a head
// slot already wearing a piece: the head takes one pumpkin, the hotbar key
// keeps the rest, and the piece it wore falls in among the player's things.
// vanilla: AbstractContainerMenu.doClick SWAP
func TestNumberKeySwapWithAWornPieceCapsTheHeadSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	helmet := servertest.ItemStack(t, "minecraft:diamond_helmet", 1)
	w.SetCreativeSlot(player.ArmorSlot, helmet)
	w.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:carved_pumpkin", 64))

	w.Click(player.ArmorSlot, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		player.ArmorSlot:      servertest.ItemStack(t, "minecraft:carved_pumpkin", 1),
		player.HotbarSlot:     servertest.ItemStack(t, "minecraft:carved_pumpkin", 63),
		player.HotbarSlot + 1: helmet,
	}, protocol.ItemStack{})

	if got := srv.InventorySlot("Testificate", player.HotbarSlot+1); !got.Equal(helmet) {
		t.Errorf("the slot the worn piece fell to = %+v, want the helmet", got)
	}
}

// TestPaintDragCapsTheHeadSlot checks a paint drag over the head slot and an
// empty pocket: the head takes one pumpkin though its share is two, and the
// pocket takes its own.
// vanilla: AbstractContainerMenu.doClick QUICK_CRAFT end step
func TestPaintDragCapsTheHeadSlot(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	pumpkins := servertest.ItemStack(t, "minecraft:carved_pumpkin", 4)
	from := int16(player.HotbarSlot + 1)
	w.SetCreativeSlot(from, pumpkins)
	w.Click(from, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{from: {}}, pumpkins)

	w.Click(-999, 0, protocol.ClickModeDrag)
	w.Drain()
	w.Click(player.ArmorSlot, 1, protocol.ClickModeDrag)
	w.Click(player.MainSlot, 1, protocol.ClickModeDrag)
	w.Drain()
	w.Click(-999, 2, protocol.ClickModeDrag)

	w.ExpectClickResult(map[int16]protocol.ItemStack{
		player.ArmorSlot: servertest.ItemStack(t, "minecraft:carved_pumpkin", 1),
		player.MainSlot:  servertest.ItemStack(t, "minecraft:carved_pumpkin", 2),
	}, servertest.ItemStack(t, "minecraft:carved_pumpkin", 1))
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
