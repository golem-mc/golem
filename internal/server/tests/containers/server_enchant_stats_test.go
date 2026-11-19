package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAnEnchantingPressCountsOnlyWhenItPays: a press the table refuses leaves
// enchant_item at nought; the press that enchants adds one.
// vanilla: EnchantmentMenu.clickMenuButton
func TestAnEnchantingPressCountsOnlyWhenItPays(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalConfig())
	srv.SetXpSeed("Testificate", testSeed)
	fillShelves(t, srv, offerRing()...)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuEnchantment, false, 4, 64, 0)
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, sword)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: sword}, protocol.ItemStack{})
	lapis := servertest.ItemStack(t, "minecraft:lapis_lazuli", 16)
	srv.GiveSlot("Testificate", player.HotbarSlot, lapis)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, lapis)
	w.Click(enchLapisSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchLapisSlot: lapis}, protocol.ItemStack{})

	stat := func() int32 { return srv.StatValue("Testificate", registry.StatCustom, "minecraft:enchant_item") }
	w.ClickButton(0)
	w.Settle()
	if got := stat(); got != 0 {
		t.Fatalf("enchant_item = %d after a press with no levels, want 0", got)
	}
	srv.SetExperienceLevels("Testificate", 10)
	w.ClickButton(0)
	w.Settle()
	if got := stat(); got != 1 {
		t.Errorf("enchant_item = %d after the paid press, want 1", got)
	}
}
