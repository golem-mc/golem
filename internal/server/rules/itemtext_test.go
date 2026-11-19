package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestADecoratedShieldIsNamedByItsBaseColour checks the shield's name reads
// its base_color, an ID out of range naming white, and a plain shield keeps
// the item's own key.
// vanilla: ShieldItem.getName
func TestADecoratedShieldIsNamedByItsBaseColour(t *testing.T) {
	shield := protocol.ItemStack{ItemID: registry.MustItem("minecraft:shield"), Count: 1}
	if got := ItemName(shield).Translate; got != "item.minecraft.shield" {
		t.Errorf("plain shield = %q, want item.minecraft.shield", got)
	}
	blue := shield
	blue.SetBaseColor(11)
	if got := ItemName(blue).Translate; got != "item.minecraft.shield.blue" {
		t.Errorf("blue shield = %q, want item.minecraft.shield.blue", got)
	}
	var value protocol.Buffer
	value.PutVarInt(99)
	outOfRange := shield
	outOfRange.Raw = []protocol.RawComponent{{Type: protocol.DataComponentBaseColor, Data: value.Bytes()}}
	if got := ItemName(outOfRange).Translate; got != "item.minecraft.shield.white" {
		t.Errorf("base colour 99 = %q, want item.minecraft.shield.white", got)
	}
}

// TestItemNameFollowsTheStacksOwnNameSources checks the potion effect keys,
// the lodestone compass, the item_name and rarity components and a custom
// name over both.
// vanilla: ItemStack.getHoverName, ItemStack.getRarity, PotionContents.getName
func TestItemNameFollowsTheStacksOwnNameSources(t *testing.T) {
	stack := func(item string) protocol.ItemStack {
		return protocol.ItemStack{ItemID: registry.MustItem(item), Count: 1}
	}
	potion := func(item, name string) protocol.ItemStack {
		s := stack(item)
		s.SetPotion(registry.MustPotion(name))
		return s
	}
	compass := stack("minecraft:compass")
	compass.SetLodestoneTracker(protocol.LodestoneTracker{})
	banner := stack("minecraft:white_banner")
	banner.SetItemName(protocol.Text{Translate: "block.minecraft.ominous_banner"})
	banner.SetRarity(1)
	for _, tc := range []struct {
		name  string
		stack protocol.ItemStack
		want  string
	}{
		{"plain potion", stack("minecraft:potion"), "item.minecraft.potion.effect.empty"},
		{"long swiftness", potion("minecraft:potion", "minecraft:long_swiftness"), "item.minecraft.potion.effect.swiftness"},
		{"splash water", potion("minecraft:splash_potion", "minecraft:water"), "item.minecraft.splash_potion.effect.water"},
		{"strong healing arrow", potion("minecraft:tipped_arrow", "minecraft:strong_healing"), "item.minecraft.tipped_arrow.effect.healing"},
		{"lodestone compass", compass, "item.minecraft.lodestone_compass"},
		{"ominous banner", banner, "block.minecraft.ominous_banner"},
	} {
		if got := ItemName(tc.stack).Translate; got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := ItemText(banner, 1).Color; got != "yellow" {
		t.Errorf("ominous banner colour = %q, want yellow", got)
	}
	named := banner
	named.CustomName = &protocol.Text{Text: "Flag"}
	if got := ItemName(named); got.Text != "Flag" || !got.Italic {
		t.Errorf("named banner = %+v, want italic Flag", got)
	}
}
