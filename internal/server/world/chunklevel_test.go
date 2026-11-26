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
