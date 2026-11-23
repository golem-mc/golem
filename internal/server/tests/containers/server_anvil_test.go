package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The anvil result's shift-click and double click, the two paths that must
// not spend the anvil when the result goes nowhere or the cursor is not its
// stack. vanilla: ItemCombinerMenu.quickMoveStack, AbstractContainerMenu
// doClick PICKUP_ALL.

// TestAnAnvilMendsWithAnEnchantedMaterialAndLeavesItsMagic puts a worn
// diamond sword and a diamond carrying Sharpness V in the anvil: the
// material mends the sword and its magic stays off the result.
// vanilla: AnvilMenu.createResult
func TestAnAnvilMendsWithAnEnchantedMaterialAndLeavesItsMagic(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 780
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("no sharpness enchantment")
	}
	diamond := servertest.ItemStack(t, "minecraft:diamond", 1)
	diamond.Enchantments = map[int32]int32{sharpness: 5}
	srv.GiveSlot("Testificate", player.HotbarSlot, diamond)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, diamond)
	w.Click(1, 0, protocol.ClickModePickup)
	mended := sword
	mended.Damage = 390
	mended.RepairCost = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: diamond, 2: mended}, protocol.ItemStack{})
}

// TestAnvilUnchangedHoverName restores an item's own rendered name in the
// anvil's name field: a translated custom name, a plain item's default name
// and the name of a potion stripped of its contents price at nothing and
// leave the result slot empty.
// vanilla: AnvilMenu.createResult (getHoverName().getString())
func TestAnvilUnchangedHoverName(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, w := servertest.OpenWindowed(t, cfg)
	w.OpenAnvil(srv)

	// put moves the left slot's stack to a fresh inventory slot, then
	// stack in.
	var held protocol.ItemStack
	parked := int16(30)
	put := func(stack protocol.ItemStack) {
		t.Helper()
		if !held.Empty() {
			w.Click(0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}}, held)
			parked++
			w.Click(parked, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{parked: held}, protocol.ItemStack{})
		}
		srv.GiveSlot("Testificate", player.HotbarSlot, stack)
		w.Drain()
		w.Click(30, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, stack)
		w.Click(0, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{0: stack}, protocol.ItemStack{})
		held = stack
	}

	steak := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	steak.CustomName = &protocol.Text{Translate: "item.minecraft.cooked_beef"}
	put(steak)
	w.RenameInAnvil("Steak", protocol.ItemStack{}, 0)
	renamed := steak
	renamed.CustomName = &protocol.Text{Text: "Steaks"}
	w.RenameInAnvil("Steaks", renamed, 1)
	w.RenameInAnvil("Steak", protocol.ItemStack{}, 0)

	put(servertest.ItemStack(t, "minecraft:diamond_sword", 1))
	w.RenameInAnvil("Diamond Sword", protocol.ItemStack{}, 0)

	// Without potion_contents the potion is named by its item_name.
	brew := servertest.ItemStack(t, "minecraft:potion", 1)
	brew.Remove(protocol.DataComponentPotionContents)
	brew.SetItemName(protocol.Text{Text: "Brew"})
	put(brew)
	w.RenameInAnvil("Brew", protocol.ItemStack{}, 0)
	renamedBrew := brew
	renamedBrew.CustomName = &protocol.Text{Text: "Uncraftable Potion"}
	w.RenameInAnvil("Uncraftable Potion", renamedBrew, 1)
}
