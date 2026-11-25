package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func oldChunkWorld(t *testing.T, version int32, blockEntities ...nbt.Tag) (*World, ChunkPos) {
	t.Helper()
	pos := ChunkPos{X: 1, Z: 2}
	root := nbt.Compound{
		"DataVersion": nbt.Int(version),
		"Status":      nbt.String("minecraft:full"),
		"xPos":        nbt.Int(pos.X),
		"zPos":        nbt.Int(pos.Z),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Y": nbt.Byte(4),
			"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String("minecraft:grass")},
			}}},
		}}},
		"block_entities": nbt.List{Elem: nbt.TypeCompound, Items: blockEntities},
	}
	encoded, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rx, rz := regionCoords(pos)
	rf, err := openRegion(regionFilePath(DimensionDir(dir, DimensionName), rx, rz), defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.writeChunk(pos, encoded); err != nil {
		t.Fatal(err)
	}
	rf.close()
	w, err := NewWorld(dir, NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	return w, pos
}

func TestOldChunkGrassBecomesShortGrass(t *testing.T) {
	t.Parallel()
	w, pos := oldChunkWorld(t, chunkDataVersionFloor)
	w.Acquire(pos)
	if got, want := w.BlockAt(pos.X*16, 64, pos.Z*16), registry.BlockStates["minecraft:short_grass"]; got != want {
		t.Errorf("block = %d, want short_grass %d", got, want)
	}
	if w.oldVersionLogged.Load() {
		t.Error("an upgradable chunk was refused")
	}
}

func TestOldChunkWithLegacyItemIsRefused(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stick"), "Count": nbt.Byte(3),
		}}},
	}
	w, pos := oldChunkWorld(t, 3465, chest)
	w.Acquire(pos)
	if !w.oldVersionLogged.Load() {
		t.Error("a chunk with a pre-1.20.5 item stack was read")
	}
}

func TestFixSignText(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{
		"id": nbt.String("minecraft:sign"), "Text1": nbt.String(`{"text":"hi"}`), "Color": nbt.String("red"),
	}
	fixSignText(tag, 3000)
	front, _ := nbt.Get[nbt.Compound](tag, "front_text")
	messages, _ := nbt.Get[nbt.List](front, "messages")
	if len(messages.Items) != 4 || messages.Items[0] != nbt.String(`{"text":"hi"}`) || messages.Items[1] != nbt.String(`""`) {
		t.Errorf("messages = %v", messages.Items)
	}
	if front["color"] != nbt.String("red") {
		t.Errorf("color = %v", front["color"])
	}
	for _, key := range []string{"Text1", "Color", "_filtered_correct"} {
		if _, ok := tag[key]; ok {
			t.Errorf("%s was kept", key)
		}
	}
	if _, ok := tag["back_text"]; !ok {
		t.Error("no back_text")
	}
}

func TestFixPotSherds(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{"shards": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
		nbt.String("minecraft:pottery_shard_archer"), nbt.String("minecraft:brick"),
		nbt.String("minecraft:angler_pottery_shard"),
	}}}
	fixPotSherds(tag, 3000)
	sherds, _ := nbt.Get[nbt.List](tag, "sherds")
	want := []string{"minecraft:archer_pottery_sherd", "minecraft:brick", "minecraft:angler_pottery_sherd"}
	for i, w := range want {
		if sherds.Items[i] != nbt.String(w) {
			t.Errorf("sherd %d = %v, want %s", i, sherds.Items[i], w)
		}
	}
	if _, ok := tag["shards"]; ok {
		t.Error("shards was kept")
	}
}
