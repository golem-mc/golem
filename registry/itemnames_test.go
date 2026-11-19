package registry_test

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestItemTranslationKey(t *testing.T) {
	for _, tc := range []struct {
		item registry.Item
		key  string
	}{
		{registry.ItemWheat, "item.minecraft.wheat"},
		{registry.ItemNetherWart, "item.minecraft.nether_wart"},
		{registry.ItemResinClump, "item.minecraft.resin_clump"},
		{registry.ItemWheatSeeds, "item.minecraft.wheat_seeds"},
		{registry.ItemDiamond, "item.minecraft.diamond"},
		{registry.ItemStone, "block.minecraft.stone"},
	} {
		if got, ok := registry.ItemTranslationKey(int32(tc.item)); !ok || got != tc.key {
			t.Errorf("ItemTranslationKey(%d) = %q, %v; want %q, true", tc.item, got, ok, tc.key)
		}
	}
	for _, id := range []int32{-1, registry.ItemCount} {
		if got, ok := registry.ItemTranslationKey(id); ok || got != "" {
			t.Errorf("ItemTranslationKey(%d) = %q, %v; want empty, false", id, got, ok)
		}
	}
}
