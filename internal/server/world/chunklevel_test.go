package world

import (
	"errors"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestUnwrapLevelChunkBiomesAndRenames(t *testing.T) {
	t.Parallel()
	old := make(nbt.IntArray, 1024)
	for i := range old {
		old[i] = 3 // mountains
	}
	old[0] = 127 // the_void
	root := nbt.Compound{"DataVersion": nbt.Int(levelChunkFloor), "Level": nbt.Compound{
		"xPos": nbt.Int(1), "Biomes": old, "TileEntities": nbt.List{Elem: nbt.TypeCompound},
		"Entities": nbt.List{Elem: nbt.TypeCompound},
	}}
	if err := unwrapLevelChunk(root, levelChunkFloor); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["Level"]; ok {
		t.Error("Level still wraps the chunk")
	}
	if _, ok := root["block_entities"]; !ok {
		t.Error("TileEntities was not renamed")
	}
	sections, _ := nbt.Get[nbt.List](root, "sections")
	if len(sections.Items) != newSectionCount {
		t.Fatalf("%d sections, want %d", len(sections.Items), newSectionCount)
	}
	bottom, _ := nbt.CompoundView(sections.Items[0])
	biomes, _ := nbt.Get[nbt.Compound](bottom, "biomes")
	palette, _ := nbt.Get[nbt.List](biomes, "palette")
	if len(palette.Items) != 2 || palette.Items[0] != nbt.String("minecraft:the_void") || palette.Items[1] != nbt.String("minecraft:windswept_hills") {
		t.Errorf("bottom palette = %v, want the_void and windswept_hills", palette.Items)
	}
}

func TestUnwrapLevelChunkKeepsWhatItCannotUpgrade(t *testing.T) {
	t.Parallel()
	root := nbt.Compound{"Level": nbt.Compound{"Entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:cow")}}}}}
	if _, ok := errors.AsType[*unsupportedVersionError](unwrapLevelChunk(root, levelChunkFloor)); !ok {
		t.Error("a chunk with entities was upgraded")
	}
}

// TestRealignBlockStates packs 4096 five-bit values across long boundaries
// the way 1.15 did and reads them back from the aligned layout.
func TestRealignBlockStates(t *testing.T) {
	t.Parallel()
	const bitsPer = 5
	old := make(nbt.LongArray, (blocksPerSection*bitsPer+63)/64)
	for i := range blocksPerSection {
		v, pos := uint64(i%20), i*bitsPer
		old[pos>>6] |= int64(v << (pos & 63))
		if pos&63+bitsPer > 64 {
			old[pos>>6+1] |= int64(v >> (64 - pos&63))
		}
	}
	got := realignBlockStates(old, 20)
	for i := range blocksPerSection {
		perLong := 64 / bitsPer
		if v := uint64(got[i/perLong]) >> (i % perLong * bitsPer) & 31; v != uint64(i%20) {
			t.Fatalf("cell %d = %d, want %d", i, v, i%20)
		}
	}
}

func TestOldPaletteAndBlockEntityFixes(t *testing.T) {
	t.Parallel()
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"Name": nbt.String("minecraft:stone_brick_wall"), "Properties": nbt.Compound{"east": nbt.String("true"), "west": nbt.String("false"), "up": nbt.String("true")}},
		nbt.Compound{"Name": nbt.String("minecraft:redstone_wire"), "Properties": nbt.Compound{"east": nbt.String("up"), "power": nbt.String("3")}},
		nbt.Compound{"Name": nbt.String("minecraft:warped_fungi")},
		nbt.Compound{"Name": nbt.String("minecraft:cauldron"), "Properties": nbt.Compound{"level": nbt.String("2")}},
	}}
	fixLegacyPalette(palette, 2230)
	wall, _ := nbt.CompoundView(palette.Items[0])
	props, _ := nbt.Get[nbt.Compound](wall, "Properties")
	if props["east"] != nbt.String("low") || props["west"] != nbt.String("none") || props["up"] != nbt.String("true") {
		t.Errorf("wall properties = %v", props)
	}
	wire, _ := nbt.CompoundView(palette.Items[1])
	props, _ = nbt.Get[nbt.Compound](wire, "Properties")
	want := nbt.Compound{"east": nbt.String("up"), "west": nbt.String("side"), "north": nbt.String("none"), "south": nbt.String("none"), "power": nbt.String("3")}
	if !nbt.Equal(props, want) {
		t.Errorf("wire properties = %v, want %v", props, want)
	}
	if name, _ := nbt.Get[nbt.String](palette.Items[2].(nbt.Compound), "Name"); name != "minecraft:warped_fungus" {
		t.Errorf("fungi renamed to %q", name)
	}
	if name, _ := nbt.Get[nbt.String](palette.Items[3].(nbt.Compound), "Name"); name != "minecraft:water_cauldron" {
		t.Errorf("cauldron renamed to %q", name)
	}

	furnace := nbt.Compound{"id": nbt.String("minecraft:furnace"), "RecipesUsedSize": nbt.Int(1),
		"RecipeLocation0": nbt.String("minecraft:iron_ingot"), "RecipeAmount0": nbt.Int(4)}
	fixOldBlockEntity(furnace, 2230)
	used, _ := nbt.Get[nbt.Compound](furnace, "RecipesUsed")
	if n, _ := nbt.IntValue(used["minecraft:iron_ingot"]); n != 4 || furnace["RecipeAmount0"] != nil {
		t.Errorf("furnace = %v", furnace)
	}
	skull := nbt.Compound{"id": nbt.String("minecraft:skull"), "Owner": nbt.Compound{"Id": nbt.String("00000001-0000-0002-0000-000000000003")}}
	fixOldBlockEntity(skull, 2230)
	owner, _ := nbt.Get[nbt.Compound](skull, "profile")
	if !nbt.Equal(owner["id"], nbt.IntArray{1, 2, 0, 3}) {
		t.Errorf("skull = %v", skull)
	}
}
