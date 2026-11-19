package registry

import "testing"

func TestEquippableAllowsEntity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item, entity string
		want         bool
	}{
		{"minecraft:iron_helmet", "minecraft:player", true},
		{"minecraft:wolf_armor", "minecraft:wolf", true},
		{"minecraft:wolf_armor", "minecraft:player", false},
		{"minecraft:white_carpet", "minecraft:trader_llama", true},
		{"minecraft:white_carpet", "minecraft:horse", false},
		{"minecraft:saddle", "minecraft:horse", true},
		{"minecraft:saddle", "minecraft:cow", false},
	} {
		if got := Equippable[tc.item].AllowsEntity(tc.entity); got != tc.want {
			t.Errorf("%s on %s: got %v, want %v", tc.item, tc.entity, got, tc.want)
		}
	}
}
