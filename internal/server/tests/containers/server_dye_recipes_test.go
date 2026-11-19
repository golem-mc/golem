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

// TestADyedBundleKeepsItsContents recolours a coloured bundle.
func TestADyedBundleKeepsItsContents(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	bundle := servertest.ItemStack(t, "minecraft:blue_bundle", 1)
	bundle.SetBundleContents([]protocol.ItemStack{servertest.ItemStack(t, "minecraft:diamond", 3)})
	w.SetCreativeSlot(36, bundle)
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:green_dye", 1))
	// A right-click with a bundle on the cursor works the bundle instead of
	// placing it, so the bundle goes in whole.
	w.Click(36, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.Click(1, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.PutInGrid(37, 2)

	want := bundle
	want.ItemID = servertest.ItemStack(t, "minecraft:green_bundle", 1).ItemID
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, want)
}

// TestADyeOfTheBoxsOwnColourMakesNothing leaves the result slot empty.
func TestADyeOfTheBoxsOwnColourMakesNothing(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:red_shulker_box", 1))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:red_dye", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
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

// TestTwoDyesMixOnDyedArmour dyes an already red helmet blue: the mix of
// the three colours keeps the average brightness.
func TestTwoDyesMixOnDyedArmour(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, dyedStack(t, "minecraft:leather_boots", 0xB02E26))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:blue_dye", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, dyedStack(t, "minecraft:leather_boots", 0xAD5398))
}

// TestDyeOnNonDyeableItemMakesNothing leaves the result slot empty.
func TestDyeOnNonDyeableItemMakesNothing(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:iron_helmet", 1))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:red_dye", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
}
