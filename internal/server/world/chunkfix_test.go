package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func oldChunkWorld(t *testing.T, version int32, blockEntities ...nbt.Tag) (*World, ChunkPos) {
	t.Helper()
	pos := ChunkPos{X: 1, Z: 2}
	block := "minecraft:grass"
	if version >= grassRenameVersion {
		block = "minecraft:stone"
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(version),
		"Status":      nbt.String("minecraft:full"),
		"xPos":        nbt.Int(pos.X),
		"zPos":        nbt.Int(pos.Z),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Y": nbt.Byte(4),
			"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String(block)},
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

func TestOldChunkWithTaggedItemIsRefused(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stick"), "Count": nbt.Byte(3),
			"tag": nbt.Compound{"display": nbt.Compound{"Name": nbt.String(`"x"`)}},
		}}},
	}
	w, pos := oldChunkWorld(t, 3465, chest)
	w.Acquire(pos)
	if !w.oldVersionLogged.Load() {
		t.Error("a chunk with a pre-1.20.5 stack with a tag was read")
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

func TestOldChestStacksAreUpgraded(t *testing.T) {
	t.Parallel()
	chest := nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(2), "id": nbt.String("minecraft:grass"), "Count": nbt.Byte(7)},
			nbt.Compound{"Slot": nbt.Byte(3), "id": nbt.String("minecraft:pottery_shard_archer"), "Count": nbt.Byte(1)},
		}},
	}
	w, pos := oldChunkWorld(t, 3400, chest)
	w.Acquire(pos)
	if w.oldVersionLogged.Load() {
		t.Fatal("an upgradable chunk was refused")
	}
	tag, ok := w.BlockEntityTag(16, 64, 32)
	if !ok {
		t.Fatal("the chest was not loaded")
	}
	items, _ := nbt.Get[nbt.List](tag, "Items")
	first, _ := nbt.CompoundView(items.Items[0])
	second, _ := nbt.CompoundView(items.Items[1])
	if first["id"] != nbt.String("minecraft:short_grass") || first["count"] != nbt.Int(7) || first["Count"] != nil {
		t.Errorf("first stack = %v", first)
	}
	if second["id"] != nbt.String("minecraft:archer_pottery_sherd") {
		t.Errorf("second stack = %v", second)
	}
}

func TestPre1215TextIsComponentsInMemoryAndJSONOnDisk(t *testing.T) {
	t.Parallel()
	line := func(s string) nbt.Tag { return nbt.String(s) }
	sign := nbt.Compound{
		"id": nbt.String("minecraft:sign"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"front_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				line(`{"text":"Hello"}`), line(`{"text":"red","color":"red","bold":true}`), line(`""`), line(`"plain"`)}},
			"color": nbt.String("black"), "has_glowing_text": nbt.Byte(0),
		},
		"back_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{line(`""`), line(`""`), line(`""`), line(`""`)}},
			"color":    nbt.String("black"), "has_glowing_text": nbt.Byte(0),
		},
		"is_waxed": nbt.Byte(0),
	}
	chest := nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(17), "y": nbt.Int(64), "z": nbt.Int(32),
		"CustomName": line(`{"text":"Loot"}`),
	}
	w, pos := oldChunkWorld(t, 4189, sign, chest)
	c, _ := w.Acquire(pos)

	name, _ := w.BlockEntityTag(17, 64, 32)
	if name["CustomName"] != nbt.String("Loot") {
		t.Errorf("CustomName in memory = %v", name["CustomName"])
	}
	signTag, _ := w.BlockEntityTag(16, 64, 32)
	front0, _ := nbt.Get[nbt.Compound](signTag, "front_text")
	if text := decodeSignText(front0); text.Messages[0] != "Hello" || text.Messages[3] != "plain" {
		t.Errorf("sign lines = %v", text.Messages)
	}

	root, err := encodeChunkNBT(c)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := nbt.Get[nbt.Int](root, "DataVersion"); v != 4189 {
		t.Fatalf("DataVersion = %v", v)
	}
	list, _ := nbt.Get[nbt.List](root, "block_entities")
	saved := map[string]nbt.Compound{}
	for _, item := range list.Items {
		v, _ := nbt.CompoundView(item)
		id, _ := nbt.Get[nbt.String](v, "id")
		saved[string(id)] = v
	}
	if got := saved["minecraft:chest"]["CustomName"]; got != nbt.String(`"Loot"`) {
		t.Errorf("saved CustomName = %v", got)
	}
	front, _ := nbt.Get[nbt.Compound](saved["minecraft:sign"], "front_text")
	messages, _ := nbt.Get[nbt.List](front, "messages")
	if len(messages.Items) != 4 || messages.Items[0] != nbt.String(`"Hello"`) {
		t.Errorf("saved messages = %v", messages.Items)
	}
	for i, item := range messages.Items {
		if s, ok := item.(nbt.String); !ok {
			t.Errorf("saved message %d is %T, want a JSON string", i, item)
		} else if parsed := unflattenText(s); parsed == nbt.Tag(s) && i == 1 {
			t.Errorf("saved message 1 %q does not parse", s)
		}
	}
}
