package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

type oneBlock uint16

func (b oneBlock) BlockAt(x, y, z int32) uint16 { return uint16(b) }

// TestHiveHarvest checks that only a full hive reads full and that emptying
// it resets the level.
func TestHiveHarvest(t *testing.T) {
	if _, full := FullHiveAt(oneBlock(stateOf(t, "minecraft:stone")), 0, 0, 0); full {
		t.Error("stone is not a hive")
	}
	if _, full := FullHiveAt(oneBlock(stateOf(t, "minecraft:bee_nest")), 0, 0, 0); full {
		t.Error("a default nest is empty")
	}
}

type column map[int32]uint16

func (c column) BlockAt(_, y, _ int32) uint16 { return c[y] }

func TestBeeGrownCrop(t *testing.T) {
	for _, tc := range []struct {
		name     string
		props    map[string]string
		attracts bool
	}{
		{"minecraft:sunflower", map[string]string{"half": "upper"}, true},
		{"minecraft:sunflower", map[string]string{"half": "lower"}, false},
		{"minecraft:flowering_azalea_leaves", map[string]string{"waterlogged": "true"}, false},
		{"minecraft:flowering_azalea_leaves", map[string]string{"waterlogged": "false"}, true},
	} {
		if got := BeeAttracts(registry.MustState(tc.name, tc.props)); got != tc.attracts {
			t.Errorf("%s %v attractiveness = %v, want %v", tc.name, tc.props, got, tc.attracts)
		}
	}
	wheat := func(age string) uint16 {
		id, _ := registry.FindState("minecraft:wheat", map[string]string{"age": age})
		return id
	}
	if got, ok := BeeGrownCrop(wheat("3")); !ok || got != wheat("4") {
		t.Errorf("age 3 wheat: %d %v", got, ok)
	}
	if _, ok := BeeGrownCrop(wheat("7")); ok {
		t.Error("ripe wheat grew")
	}
	if _, ok := BeeGrownCrop(stateOf(t, "minecraft:stone")); ok {
		t.Error("stone grew")
	}
	if !BeeAttracts(stateOf(t, "minecraft:dandelion")) || BeeAttracts(stateOf(t, "minecraft:stone")) {
		t.Error("attractiveness")
	}
}
