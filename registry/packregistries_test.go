package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestApplyPackRegistriesSkipsWhatTheServerCannotModel checks that a new
// biome and an override of a built-in dimension type are left out, while a
// new dimension type and a biome override load. Not parallel: the table is
// process-wide.
func TestApplyPackRegistriesSkipsWhatTheServerCannotModel(t *testing.T) {
	t.Cleanup(func() { ApplyPackRegistries(nil) })
	data := nbt.Compound{"x": nbt.Int(1)}
	problems := ApplyPackRegistries(map[string]map[string]nbt.Tag{
		"minecraft:worldgen/biome": {"pk:new": data, "minecraft:plains": data},
		"minecraft:dimension_type": {"pk:new": data, "minecraft:overworld": data},
	})
	if len(problems) != 2 {
		t.Errorf("problems = %q, want the new biome and the dimension type override", problems)
	}
	biomes, _ := Get("minecraft:worldgen/biome")
	if biomes.ID("pk:new") >= 0 || biomes.Data["minecraft:plains"] == nil || len(biomes.Data) != 1 {
		t.Errorf("biomes: pk:new at %d, data %v; want no pk:new and plains' data", biomes.ID("pk:new"), biomes.Data)
	}
	dims, _ := Get("minecraft:dimension_type")
	if dims.Entries[len(dims.Entries)-1] != "pk:new" || dims.Data["minecraft:overworld"] != nil || dims.Data["pk:new"] == nil {
		t.Errorf("dimension types: pk:new at %d, data %v; want pk:new appended with data and no overworld data", dims.ID("pk:new"), dims.Data)
	}
}
