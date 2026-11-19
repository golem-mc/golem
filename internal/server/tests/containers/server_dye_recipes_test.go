package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The dyeing crafting recipes: a shulker box or bundle plus a dye, and
// dyeable armour plus any dyes. vanilla: TransmuteRecipe, DyeRecipe

func dyedStack(t *testing.T, name string, rgb int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, name, 1)
	stack.SetDyedColor(rgb)
	return stack
}

// TestADyeColoursAShulkerBoxKeepingItsContents crafts a shulker box holding
// diamonds with red dye: the red box comes out with the same contents.
func TestADyeColoursAShulkerBoxKeepingItsContents(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	box := servertest.ItemStack(t, "minecraft:shulker_box", 1)
	box.Container = []protocol.ItemStack{servertest.ItemStack(t, "minecraft:diamond", 3)}
	w.SetCreativeSlot(36, box)
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:red_dye", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	want := box
	want.ItemID = servertest.ItemStack(t, "minecraft:red_shulker_box", 1).ItemID
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, want)
}

// TestADyeColoursLeatherArmour gives the helmet the dye's texture colour.
func TestADyeColoursLeatherArmour(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:leather_helmet", 1))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:red_dye", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, dyedStack(t, "minecraft:leather_helmet", 0xB02E26))
}
