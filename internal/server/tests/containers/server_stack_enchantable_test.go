package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func componentTag(key string, value nbt.Tag) nbt.Tag {
	c := nbt.NewOrderedCompound(1)
	c.Set(key, value)
	return c
}

// TestAStacksOwnEnchantableDrivesTheTable: a stick given enchantable=10 gets
// three priced offers on the table, a sword whose enchantable is removed none.
// vanilla: ItemStack.isEnchantable, EnchantmentHelper.getEnchantmentCost
func TestAStacksOwnEnchantableDrivesTheTable(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.SetXpSeed("Testificate", testSeed)
	fillShelves(t, srv, offerRing()...)
	openTable(t, srv, w, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	if !registry.ReadComponent(&stick, "minecraft:enchantable", componentTag("value", nbt.Int(10))) {
		t.Fatal("enchantable={value:10} was refused")
	}
	w.SetCreativeSlot(36, stick)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, stick)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: stick}, protocol.ItemStack{})
	props := servertest.CollectWindowProps(w)
	for _, p := range []int32{protocol.EnchantPropertyCost0, protocol.EnchantPropertyCost1, protocol.EnchantPropertyCost2} {
		if props[p] <= 0 {
			t.Errorf("stick[enchantable=10] cost property %d = %d, want an offer; got %v", p, props[p], props)
		}
	}

	// Take the stick back and lay down a sword that lost its enchantable.
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: {}}, stick)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: stick}, protocol.ItemStack{})
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Remove(protocol.DataComponentEnchantable)
	srv.GiveSlot("Testificate", player.HotbarSlot+1, sword)
	w.Drain()
	w.Click(enchHotbar1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar1: {}}, sword)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: sword}, protocol.ItemStack{})
	props = servertest.CollectWindowProps(w)
	for _, p := range []int32{protocol.EnchantPropertyCost0, protocol.EnchantPropertyCost1, protocol.EnchantPropertyCost2} {
		if props[p] != 0 {
			t.Errorf("sword[!enchantable] cost property %d = %d, want 0", p, props[p])
		}
	}
}
