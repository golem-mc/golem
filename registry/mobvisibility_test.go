package registry

import (
	"slices"
	"testing"
)

func TestItemMobVisibilityDefaults(t *testing.T) {
	for _, tc := range []struct {
		item  Item
		types []string
	}{
		{ItemSkeletonSkull, []string{"minecraft:skeleton"}},
		{ItemZombieHead, []string{"minecraft:zombie"}},
		{ItemCreeperHead, []string{"minecraft:creeper"}},
		{ItemPiglinHead, []string{"minecraft:piglin", "minecraft:piglin_brute"}},
	} {
		types, visibility, ok := tc.item.MobVisibility()
		if !ok || visibility != 0.5 || !slices.Equal(types, tc.types) {
			t.Errorf("%s: MobVisibility = %v, %v, %v; want %v, 0.5, true", tc.item.Name(), types, visibility, ok, tc.types)
		}
	}
	for _, item := range []Item{ItemAir, ItemIronHelmet, ItemDragonHead, ItemWitherSkeletonSkull, -1, ItemCount} {
		types, visibility, ok := item.MobVisibility()
		if ok || types != nil || visibility != 0 {
			t.Errorf("%d: MobVisibility = %v, %v, %v; want nil, 0, false", item, types, visibility, ok)
		}
	}
}
