package world

import (
	"slices"
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

func TestFixOldChunkRootStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version              int32
		status, want, target string
	}{
		{2975, "full", "minecraft:full", "minecraft:spawn"},
		{3449, "liquid_carvers", "minecraft:carvers", "minecraft:spawn"},
		{chunkStatusVersion, "minecraft:heightmaps", "minecraft:heightmaps", "heightmaps"},
	} {
		root := nbt.Compound{
			"DataVersion":         nbt.Int(tc.version),
			"Status":              nbt.String(tc.status),
			"below_zero_retrogen": nbt.Compound{"target_status": nbt.String("heightmaps")},
		}
		fixOldChunkRoot(root)
		if got, _ := nbt.Get[nbt.String](root, "Status"); string(got) != tc.want {
			t.Errorf("version %d: status %q became %q, want %q", tc.version, tc.status, got, tc.want)
		}
		retrogen, _ := nbt.Get[nbt.Compound](root, "below_zero_retrogen")
		if got, _ := nbt.Get[nbt.String](retrogen, "target_status"); string(got) != tc.target {
			t.Errorf("version %d: target status = %q, want %q", tc.version, got, tc.target)
		}
	}
	if err := checkChunkFormat(nbt.Compound{"DataVersion": nbt.Int(chunkStatusVersion), "Status": nbt.String("full")}); err == nil {
		t.Error("an unnamespaced status from a version that namespaces it was read")
	}
	if err := checkChunkFormat(nbt.Compound{"DataVersion": nbt.Int(2975), "Status": nbt.String("full")}); err != nil {
		t.Errorf("a 1.18.2 status was refused: %v", err)
	}
}

func TestFixStructureNames(t *testing.T) {
	t.Parallel()
	biomes := func(names ...string) nbt.Compound {
		items := make([]nbt.Tag, len(names))
		for i, name := range names {
			items[i] = nbt.String(name)
		}
		return nbt.Compound{"biomes": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeString, Items: items}}}
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(2865),
		"Status":      nbt.String("full"),
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			biomes("minecraft:plains", "minecraft:desert"),
			biomes("minecraft:desert", "minecraft:taiga"),
			// A tie between ocean_ruin's one mapping and nothing else keeps it.
			biomes("minecraft:warm_ocean"),
		}},
		"structures": nbt.Compound{
			"starts": nbt.Compound{
				"Village":   nbt.Compound{"id": nbt.String("village"), "ChunkX": nbt.Int(1)},
				"mineshaft": nbt.Compound{"id": nbt.String("INVALID")},
				"Shipwreck": nbt.Compound{"id": nbt.String("shipwreck")},
				"Fortress":  nbt.Compound{},
				"unknown":   nbt.Compound{"id": nbt.String("unknown")},
			},
			"References": nbt.Compound{
				"village":       nbt.LongArray{1},
				"Ocean_Ruin":    nbt.LongArray{4},
				"ruined_portal": nbt.LongArray{},
				"EndCity":       nbt.LongArray{2, 3},
				"unknown":       nbt.LongArray{5},
			},
		},
	}
	fixOldChunkRoot(root)
	structures, _ := nbt.Get[nbt.Compound](root, "structures")
	starts, _ := nbt.Get[nbt.Compound](structures, "starts")
	if len(starts) != 2 {
		t.Errorf("starts = %v, want the village and the shipwreck", starts)
	}
	village, _ := nbt.Get[nbt.Compound](starts, "minecraft:village_desert")
	if id, _ := nbt.Get[nbt.String](village, "id"); id != "minecraft:village_desert" || village["ChunkX"] != nbt.Int(1) {
		t.Errorf("village start = %v, want a village_desert start keeping its fields", village)
	}
	if _, ok := starts["minecraft:shipwreck"]; !ok {
		t.Errorf("starts = %v, want the shipwreck under its fallback name", starts)
	}
	refs, _ := nbt.Get[nbt.Compound](structures, "References")
	want := map[string]nbt.LongArray{
		"minecraft:village_desert":  {1},
		"minecraft:ocean_ruin_warm": {4},
		"minecraft:end_city":        {2, 3},
	}
	if len(refs) != len(want) {
		t.Errorf("references = %v, want %v", refs, want)
	}
	for name, list := range want {
		if got, _ := refs[name].(nbt.LongArray); !slices.Equal(got, list) {
			t.Errorf("references[%s] = %v, want %v", name, refs[name], list)
		}
	}
}

func TestFixOldBlockEntityFields(t *testing.T) {
	t.Parallel()
	beacon := nbt.Compound{"id": nbt.String("minecraft:beacon"), "Primary": nbt.Int(1), "Secondary": nbt.Int(0), "Levels": nbt.Int(4)}
	fixOldBlockEntity(beacon, 3465)
	if got, _ := nbt.Get[nbt.String](beacon, "primary_effect"); got != "minecraft:speed" {
		t.Errorf("primary effect = %q, want speed", got)
	}
	for _, key := range []string{"Primary", "Secondary", "secondary_effect"} {
		if _, ok := beacon[key]; ok {
			t.Errorf("beacon kept %s: %v", key, beacon)
		}
	}

	sand := nbt.Compound{"id": nbt.String("minecraft:suspicious_sand"), "loot_table": nbt.String("minecraft:archaeology/desert_pyramid"), "loot_table_seed": nbt.Long(7)}
	fixOldBlockEntity(sand, 3337)
	if sand["id"] != nbt.String("minecraft:brushable_block") || sand["LootTable"] == nil || sand["LootTableSeed"] != nbt.Long(7) || sand["loot_table"] != nil {
		t.Errorf("suspicious sand = %v, want a brushable block with LootTable and LootTableSeed", sand)
	}

	sign := nbt.Compound{"id": nbt.String("minecraft:sign"), "Text1": nbt.String(`"a"`), "FilteredText1": nbt.String(`"b"`)}
	fixOldBlockEntity(sign, 2975)
	front, _ := nbt.Get[nbt.Compound](sign, "front_text")
	if _, ok := front["filtered_messages"]; ok {
		t.Errorf("front text = %v, want the filtered lines a 1.18.2 sign holds dropped", front)
	}
}

func TestDropOldBlendingData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version   int32
		dimension string
		kept      bool
	}{
		{2975, "minecraft:the_nether", false},
		{2975, DimensionName, true},
		{blendingDataVersion, "minecraft:the_end", true},
	} {
		root := nbt.Compound{"DataVersion": nbt.Int(tc.version), "blending_data": nbt.Compound{}}
		dropOldBlendingData(root, tc.dimension)
		if _, ok := root["blending_data"]; ok != tc.kept {
			t.Errorf("version %d in %s: blending data kept %v, want %v", tc.version, tc.dimension, ok, tc.kept)
		}
	}
}

// TestOldChunkClaimsTheFixedVersion checks that a chunk the fixes brought up
// to oldChunkFixedVersion is saved claiming it, so they do not run again over
// their own output, while a newer chunk keeps its claim.
func TestOldChunkClaimsTheFixedVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, want int32
		status        string
	}{
		{2975, oldChunkFixedVersion, "full"},
		{3700, 3700, "minecraft:full"},
	} {
		c, err := decodeChunkNBT(nbt.Compound{
			"DataVersion": nbt.Int(tc.version),
			"Status":      nbt.String(tc.status),
			"xPos":        nbt.Int(0),
			"zPos":        nbt.Int(0),
		})
		if err != nil || c == nil {
			t.Fatalf("version %d: decode = %v, %v", tc.version, c, err)
		}
		if got := c.claimedVersion(); got != tc.want {
			t.Errorf("version %d: claimed %d, want %d", tc.version, got, tc.want)
		}
	}
}
