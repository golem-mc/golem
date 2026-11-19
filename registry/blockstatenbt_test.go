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

// TestBlockStateOfIsLenient checks that a missing name reads as "", that a
// property of another type or a Properties of another shape is skipped, and
// that the map is never nil.
func TestBlockStateOfIsLenient(t *testing.T) {
	t.Parallel()
	name, props := BlockStateOf(nbt.Compound{
		"Properties": nbt.Compound{"lit": nbt.String("true"), "level": nbt.Int(3)},
	})
	if name != "" || !maps.Equal(props, map[string]string{"lit": "true"}) {
		t.Errorf("BlockStateOf(no name, mixed props) = %q %v, want \"\" map[lit:true]", name, props)
	}
	name, props = BlockStateOf(nbt.Compound{"Name": nbt.Int(1), "Properties": nbt.String("lit=true")})
	if name != "" || props == nil || len(props) != 0 {
		t.Errorf("BlockStateOf(wrong types) = %q %v, want \"\" and an empty map", name, props)
	}
	if _, props := BlockStateOf(nil); props == nil {
		t.Error("BlockStateOf(nil) gave a nil map")
	}
}

// TestBlockStateOfForms checks the three forms a reader accepts: id/properties,
// the legacy Name/Properties and a bare name.
func TestBlockStateOfForms(t *testing.T) {
	t.Parallel()
	want := map[string]string{"axis": "x"}
	for name, tag := range map[string]nbt.Tag{
		"26.3":   nbt.Compound{"id": nbt.String("minecraft:bone_block"), "properties": nbt.Compound{"axis": nbt.String("x")}},
		"legacy": nbt.Compound{"Name": nbt.String("minecraft:bone_block"), "Properties": nbt.Compound{"axis": nbt.String("x")}},
	} {
		if got, props := BlockStateOf(tag); got != "minecraft:bone_block" || !maps.Equal(props, want) {
			t.Errorf("%s: BlockStateOf = %q %v", name, got, props)
		}
	}
	if got, props := BlockStateOf(nbt.String("minecraft:stone")); got != "minecraft:stone" || len(props) != 0 {
		t.Errorf("bare: BlockStateOf = %q %v", got, props)
	}
}

// vanilla: StateDefinition.appendPropertyCodec
func TestBlockStateCodecPropertyFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		props nbt.Compound
		want  map[string]string
	}{
		{"unknown key", nbt.Compound{"facing": nbt.String("east"), "unknown": nbt.String("true")}, map[string]string{"facing": "east"}},
		{"invalid value", nbt.Compound{"facing": nbt.String("east"), "half": nbt.String("invalid")}, map[string]string{"facing": "east"}},
		{"wrong type", nbt.Compound{"facing": nbt.String("east"), "half": nbt.Int(1)}, map[string]string{"facing": "east"}},
		{"mixed", nbt.Compound{"facing": nbt.String("east"), "half": nbt.String("top"), "shape": nbt.String("invalid"), "unknown": nbt.String("true")}, map[string]string{"facing": "east", "half": "top"}},
		{"only invalid", nbt.Compound{"facing": nbt.String("invalid"), "unknown": nbt.String("true")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, keys := range [][2]string{{"id", "properties"}, {"Name", "Properties"}} {
				name, props := BlockStateOf(nbt.Compound{keys[0]: nbt.String("minecraft:oak_stairs"), keys[1]: tc.props})
				got, ok := FindState(name, props)
				want := MustState("minecraft:oak_stairs", tc.want)
				if !ok || got != want {
					t.Errorf("%s: decoded state = %d, %v; want %d", keys[0], got, ok, want)
				}
			}
		})
	}
}
