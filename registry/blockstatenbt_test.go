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

func TestBlockStateOfCodec(t *testing.T) {
	t.Parallel()
	if name, props, ok := BlockStateOfCodec(nbt.String("minecraft:stone")); !ok || name != "minecraft:stone" || props != nil {
		t.Errorf("bare string = (%q, %v, %t), want minecraft:stone nil true", name, props, ok)
	}
	codecTag := nbt.Compound{
		"id":         nbt.String("minecraft:oak_log"),
		"properties": nbt.Compound{"axis": nbt.String("x")},
	}
	if name, props, ok := BlockStateOfCodec(codecTag); !ok || name != "minecraft:oak_log" || !maps.Equal(props, map[string]string{"axis": "x"}) {
		t.Errorf("codec compound = (%q, %v, %t), want minecraft:oak_log map[axis:x] true", name, props, ok)
	}
	// Malformed properties or non-string values are ignored so FindState defaults them.
	for _, malformed := range []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:oak_log"), "properties": nbt.Int(1)},
		nbt.Compound{"id": nbt.String("minecraft:oak_log"), "properties": nbt.String("axis=x")},
		nbt.Compound{"id": nbt.String("minecraft:oak_log"), "properties": nbt.Compound{"axis": nbt.Int(1)}},
	} {
		if name, props, ok := BlockStateOfCodec(malformed); !ok || name != "minecraft:oak_log" || props != nil {
			t.Errorf("BlockStateOfCodec(%#v) = (%q, %v, %t), want minecraft:oak_log nil true", malformed, name, props, ok)
		}
	}
	for _, bad := range []nbt.Tag{
		nbt.Compound{"Name": nbt.String("minecraft:oak_log"), "Properties": nbt.Compound{"axis": nbt.String("x")}},
		nbt.Int(1),
		nil,
	} {
		if name, props, ok := BlockStateOfCodec(bad); ok {
			t.Errorf("BlockStateOfCodec(%#v) = (%q, %v, true), want false", bad, name, props)
		}
	}
}
