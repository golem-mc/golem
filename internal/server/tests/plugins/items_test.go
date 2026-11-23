package plugins_test

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemStack.CODEC, ItemParser, MaxStackSize.
func TestPluginItemsParseFormat(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		items := h.Items()
		s, err := items.Parse("diamond_sword[damage=3,enchantments={sharpness:5}]", 2)
		if err != nil {
			t.Fatal(err)
		}
		if s.Item != registry.ItemDiamondSword || s.Count != 2 || s.Patch.Damage == nil || *s.Patch.Damage != 3 {
			t.Errorf("parsed %+v", s)
		}
		if len(s.Patch.Enchantments) != 1 {
			t.Errorf("enchantments %v", s.Patch.Enchantments)
		}
		again, err := items.Parse(items.Format(s), 2)
		if err != nil {
			t.Fatalf("format %q: %v", items.Format(s), err)
		}
		if !reflect.DeepEqual(again, s) {
			t.Errorf("round trip %+v != %+v", again, s)
		}
		if !items.Similar(s, again) || items.Similar(s, plugin.NewStack(registry.ItemDiamondSword, 2)) {
			t.Error("Similar wrong")
		}
		for _, glint := range []bool{false, true} {
			sword := plugin.NewStack(registry.ItemDiamondSword, 1)
			sword.Patch.GlintOverride = &glint
			text := items.Format(sword)
			parsed, err := items.Parse(text, sword.Count)
			if err != nil || !reflect.DeepEqual(parsed, sword) {
				t.Errorf("glint %t: parsing %q = %+v, %v", glint, text, parsed, err)
			}
		}
		if _, err := items.Parse("not_an_item", 1); err == nil {
			t.Error("unknown item parsed")
		}
		if _, err := items.Parse("diamond_sword[bogus=1]", 1); err == nil {
			t.Error("unknown component parsed")
		}
		if _, err := items.Parse("diamond_sword[enchantment_glint_override=1:bad]", 1); err == nil {
			t.Error("resource location parsed as a glint override")
		}
		if got := items.MaxStackSize(plugin.NewStack(registry.ItemEnderPearl, 1)); got != 16 {
			t.Errorf("ender pearl max %d", got)
		}
		stick := plugin.NewStack(registry.ItemStick, 1)
		n := 5
		stick.Patch.MaxStackSize = &n
		if got := items.MaxStackSize(stick); got != 5 {
			t.Errorf("override max %d", got)
		}
		if err := items.Check(stick); err != nil {
			t.Error(err)
		}
		if items.Name(plugin.NewStack(registry.ItemStick, 1)).PlainString() == "" {
			t.Error("empty name")
		}
	})
}

// vanilla: EnchantmentHelper.enchantItem.
func TestPluginItemsEnchant(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		items := h.Items()
		sword := items.Enchant(plugin.NewStack(registry.ItemDiamondSword, 1), 30, false)
		if len(sword.Patch.Enchantments) == 0 {
			t.Error("sword not enchanted")
		}
		book := items.Enchant(plugin.NewStack(registry.ItemBook, 1), 30, true)
		if book.Item != registry.ItemEnchantedBook || len(book.Patch.StoredEnchantments) == 0 {
			t.Errorf("book %+v", book)
		}
	})
}
