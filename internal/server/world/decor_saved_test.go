package world

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// Generated carried block entities are kept as vanilla saves them once loaded:
// an empty components map, and a vault's config without its default fields
// next to empty server and shared data.
func TestGeneratedCarriedEntitiesSaveAsVanilla(t *testing.T) {
	c := newChunk(0, 0)
	for i, tag := range []nbt.Compound{
		{"id": nbt.String("minecraft:vault"), "config": nbt.Compound{
			"key_item":   nbt.Compound{"count": nbt.Int(1), "id": nbt.String("minecraft:trial_key")},
			"loot_table": nbt.String("minecraft:chests/trial_chambers/reward"),
		}},
		{"id": nbt.String("minecraft:vault"), "config": nbt.Compound{
			"loot_table":       nbt.String("minecraft:chests/trial_chambers/reward_ominous"),
			"activation_range": nbt.Double(4),
		}},
		{"id": nbt.String("minecraft:trial_spawner"), "normal_config": nbt.String("minecraft:trial_chamber/melee/zombie/normal")},
		{"id": nbt.String("minecraft:brushable_block"), "LootTable": nbt.String("minecraft:archaeology/ocean_ruin_cold")},
	} {
		protoEntity{x: int32(i), y: 0, z: 0, tag: tag}.place(c, worldMinY)
	}
	c.setCarriedBlockEntity(hiveEntry(4, worldMinY, 0, 1<<30))

	want := []nbt.Compound{
		{"id": nbt.String("minecraft:vault"), "components": nbt.Compound{}, "server_data": nbt.Compound{}, "shared_data": nbt.Compound{},
			"config": nbt.Compound{"key_item": nbt.Compound{"count": nbt.Int(1), "id": nbt.String("minecraft:trial_key")}}},
		{"id": nbt.String("minecraft:vault"), "components": nbt.Compound{}, "server_data": nbt.Compound{}, "shared_data": nbt.Compound{},
			"config": nbt.Compound{"loot_table": nbt.String("minecraft:chests/trial_chambers/reward_ominous")}},
		{"id": nbt.String("minecraft:trial_spawner"), "components": nbt.Compound{},
			"normal_config": nbt.String("minecraft:trial_chamber/melee/zombie/normal")},
		{"id": nbt.String("minecraft:brushable_block"), "components": nbt.Compound{},
			"LootTable": nbt.String("minecraft:archaeology/ocean_ruin_cold")},
		{"id": nbt.String("minecraft:beehive"), "components": nbt.Compound{}},
	}
	for i, w := range want {
		got, ok := c.carriedBlockEntityAt(int32(i), worldMinY, 0)
		if !ok {
			t.Fatalf("no carried entity at x=%d", i)
		}
		got = withoutPos(got)
		if i == 4 {
			delete(got, "bees")
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("x=%d saved\n%#v\nwant\n%#v", i, got, w)
		}
	}
}

// withoutPos drops the position keys of a carried compound.
func withoutPos(c nbt.Compound) nbt.Compound {
	out := nbt.Copy(c).(nbt.Compound)
	delete(out, "x")
	delete(out, "y")
	delete(out, "z")
	return out
}
