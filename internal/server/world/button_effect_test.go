package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestButtonEffectSoundPerFamily(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"minecraft:oak_button":                 "wooden_button",
		"minecraft:spruce_button":              "wooden_button",
		"minecraft:pale_oak_button":            "wooden_button",
		"minecraft:stone_button":               "stone_button",
		"minecraft:polished_blackstone_button": "stone_button",
		"minecraft:cherry_button":              "cherry_wood_button",
		"minecraft:bamboo_button":              "bamboo_wood_button",
		"minecraft:crimson_button":             "nether_wood_button",
		"minecraft:warped_button":              "nether_wood_button",
	}
	for name, family := range cases {
		b, ok := registry.BlockByName(name)
		if !ok {
			t.Fatalf("unknown block %s", name)
		}
		for _, on := range []bool{true, false} {
			click := "click_off"
			if on {
				click = "click_on"
			}
			e := buttonEffect(b, on, 1, 2, 3)
			if want := "minecraft:block." + family + "." + click; e.SoundID.Name() != want {
				t.Errorf("%s on=%v: sound %q, want %q", name, on, e.SoundID.Name(), want)
			}
			if e.Volume != 1 || e.Pitch != 1 {
				t.Errorf("%s: volume %v pitch %v, want 1 and 1", name, e.Volume, e.Pitch)
			}
		}
	}
}
