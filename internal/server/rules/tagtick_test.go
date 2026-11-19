package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// Resolving tag IDs once must still observe membership changes on reload.
func TestTickRulesFollowReloadedBlockTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	stone := registry.BlockStone.DefaultState()
	wheat := registry.MustState("minecraft:wheat", map[string]string{"age": "3"})
	check := func(added bool) {
		t.Helper()
		if got := BeeAttracts(stone); got != added {
			t.Errorf("stone attracts bees = %v, want %v", got, added)
		}
		if got := stateMatches(stone, "#minecraft:soul_speed_blocks", nil); got != added {
			t.Errorf("stone supports soul speed = %v, want %v", got, added)
		}
		if got := BeaconPyramidLevel(oneBlock(stone), 0, 64, 0, 0); (got == 4) != added {
			t.Errorf("stone pyramid level = %d, added = %v", got, added)
		}
		_, grows := BeeGrownCrop(wheat)
		if grows == added {
			t.Errorf("wheat grows = %v, removed = %v", grows, added)
		}
	}
	check(false)
	entry := registry.PackTag{Entries: []registry.PackTagEntry{{ID: "minecraft:stone", Required: true}}}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:bee_attractive":     entry,
		"minecraft:soul_speed_blocks":  entry,
		"minecraft:beacon_base_blocks": entry,
		"minecraft:bee_growables":      {Replace: true},
	}})
	check(true)
	registry.ApplyPackTags(nil)
	check(false)
}
