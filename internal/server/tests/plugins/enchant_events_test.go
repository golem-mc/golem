package plugins_test

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EnchantmentMenu.slotsChanged, EnchantmentMenu.clickMenuButton.
func TestPluginEnchantEvents(t *testing.T) {
	t.Parallel()
	srv, h, run, w := inventoryPlayer(t)
	ow := srv.OverworldWorld()
	ow.Acquire(worldChunk0)
	defer ow.Release(worldChunk0)
	ow.SetBlock(4, 64, 0, registry.BlockEnchantingTable.DefaultState())
	const sharpness = registry.Enchantment(33)
	var prepared, enchanted atomic.Int32
	var press *plugin.EnchantItemEvent
	cancel := true
	run(func() {
		plugin.Listen(h, func(e *plugin.PrepareItemEnchantEvent) {
			prepared.Add(1)
			e.Offers[0].Cost = 3
		})
		plugin.Listen(h, func(e *plugin.EnchantItemEvent) {
			enchanted.Add(1)
			press = e
			e.SetCancelled(cancel)
			e.Enchantments = map[plugin.Enchantment]int{sharpness: 5}
		})
	})
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuEnchantment, false, 4, 64, 0)
	hotbar := int16(2 + (player.HotbarSlot - player.MainSlot))
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	w.SetCreativeSlot(36, sword)
	w.Click(hotbar, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})
	servertest.UntilTicks(t, srv, 20, "the prepare event", func() bool { return prepared.Load() != 0 })

	w.ClickButton(0)
	servertest.UntilTicks(t, srv, 20, "the enchant event", func() bool { return enchanted.Load() == 1 })
	w.AwaitWindowSlot(0, sword)
	// The press reads the listener's edited offer cost.
	if press.Button != 0 || press.Cost != 3 || press.Item.Item != registry.ItemDiamondSword || len(press.Enchantments) == 0 {
		t.Errorf("enchant event %+v", press)
	}

	cancel = false
	w.ClickButton(0)
	want := sword
	want.Enchantments = map[int32]int32{int32(sharpness): 5}
	w.AwaitWindowSlot(0, want)
}
