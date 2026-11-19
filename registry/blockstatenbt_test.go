package registry

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestBlockStateTagRoundTrips checks that a state's name and properties come
// back from the compound BlockStateTag writes, and that a state without
// properties writes no Properties compound.
func TestBlockStateTagRoundTrips(t *testing.T) {
	t.Parallel()
	tag := BlockStateTag("minecraft:oak_stairs", "facing=east,half=top,shape=straight,waterlogged=false")
	props, ok := nbt.Get[nbt.Compound](tag, "properties")
	if !ok || len(props) != 4 || props["facing"] != nbt.String("east") {
		t.Fatalf("Properties = %v, want four string values with facing=east", tag["properties"])
	}
	name, read := BlockStateOf(tag)
	want := map[string]string{"facing": "east", "half": "top", "shape": "straight", "waterlogged": "false"}
	if name != "minecraft:oak_stairs" || !maps.Equal(read, want) {
		t.Errorf("BlockStateOf = %q %v, want minecraft:oak_stairs %v", name, read, want)
	}

	bare := BlockStateTag("minecraft:stone", "")
	if _, has := bare["properties"]; has || len(bare) != 1 {
		t.Errorf("BlockStateTag(stone, \"\") = %v, want the name alone", bare)
	}
}
