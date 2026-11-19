package world

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDoorSoundIDs(t *testing.T) {
	for block := range registry.Block(registry.BlockCount) {
		if !isDoorLike[block] {
			continue
		}
		name := strings.TrimPrefix(block.Name(), "minecraft:")
		kind := doorKindOf(block)
		family := "wooden_"
		if kind == "fence_gate" {
			family = ""
		}
		switch {
		case strings.HasPrefix(name, "bamboo_"):
			family = "bamboo_wood_"
		case strings.HasPrefix(name, "cherry_"):
			family = "cherry_wood_"
		case strings.HasPrefix(name, "crimson_"), strings.HasPrefix(name, "warped_"):
			family = "nether_wood_"
		case strings.Contains(name, "copper_"):
			family = "copper_"
		case strings.HasPrefix(name, "iron_"):
			family = "iron_"
		}
		for i, edge := range []string{"close", "open"} {
			want := "minecraft:block." + family + kind + "." + edge
			if got := doorSounds[block][i]; got.Name() != want {
				t.Errorf("%s %s: got %d (%s), want %s", name, edge, got, got.Name(), want)
			}
		}
	}
}
