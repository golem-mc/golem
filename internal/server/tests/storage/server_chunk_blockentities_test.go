package storage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// chunkBlockEntity is one entry of a Chunk Data packet's block entity list:
// its section-relative position, its type in the client's registry, and the
// update tag it carries (nil for an entity whose type has none).
type chunkBlockEntity struct {
	packedXZ uint8
	y        int16
	typeID   int32
	tag      nbt.Compound
}

// chunkBlockEntities reads the block entity list out of a Chunk Data packet.
func chunkBlockEntities(t *testing.T, packet []byte) []chunkBlockEntity {
	t.Helper()
	_, r := servertest.ParseHeightmaps(t, packet)
	r.ReadBytes(1 << 24) // the chunk's sections
	count := int(r.ReadVarInt())
	if err := r.Err(); err != nil {
		t.Fatalf("chunk data head: %v", err)
	}
	out := make([]chunkBlockEntity, 0, count)
	data := r.Rest()
	off := 0
	for range count {
		if off+3 > len(data) {
			t.Fatalf("block entity list ran short after %d entries", len(out))
		}
		e := chunkBlockEntity{packedXZ: data[off], y: int16(binary.BigEndian.Uint16(data[off+1:]))}
		off += 3
		typeID, n := varIntAt(data, off)
		off += n
		e.typeID = typeID
		if off >= len(data) {
			t.Fatalf("block entity entry has no tag")
		}
		if data[off] == 0 {
			// Vanilla's writeNbt(null): an entity whose update tag is
			// empty is listed with no tag at all.
			off++
			out = append(out, e)
			continue
		}
		tag, consumed, err := nbt.DecodeNetwork(data[off:])
		if err != nil {
			t.Fatalf("block entity %d NBT: %v", len(out), err)
		}
		compound, ok := tag.(nbt.Compound)
		if !ok {
			t.Fatalf("block entity %d NBT = %#v, want a compound", len(out), tag)
		}
		e.tag = compound
		off += consumed
		out = append(out, e)
	}
	return out
}

// varIntAt reads a VarInt from data at off and returns it with the bytes read.
func varIntAt(data []byte, off int) (int32, int) {
	var v uint32
	var shift uint
	for i := 0; ; i++ {
		v |= uint32(data[off+i]&0x7F) << shift
		if data[off+i]&0x80 == 0 {
			return int32(v), i + 1
		}
		shift += 7
	}
}

// blockEntityAt finds the entry whose packed position is the block at x, z in
// the chunk and whose Y is y, or fails the test.
func blockEntityAt(t *testing.T, entries []chunkBlockEntity, x, y, z int32) chunkBlockEntity {
	t.Helper()
	packed := uint8(x&0xF)<<4 | uint8(z&0xF)
	for _, e := range entries {
		if e.packedXZ == packed && e.y == int16(y) {
			return e
		}
	}
	t.Fatalf("no block entity entry at (%d,%d,%d) among %d entries", x, y, z, len(entries))
	return chunkBlockEntity{}
}

// TestChunkDataCarriesBlockEntities writes a range of block entities into a
// saved chunk's region file, restarts the server over it and reads the block
// entity list off the Chunk Data packet: each reaches the client with the
// update tag its vanilla class builds, the chunk file's own keepPacked and
// components keys dropped (the banner keeps components), and a type with an
// empty tag listed with no tag at all.
func TestChunkDataCarriesBlockEntities(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	// A first run saves chunk (0,0) to its region file: the torch is the one
	// change that writes it out before any player joins. The trial spawners'
	// block state decides whether their next spawn travels, so it goes into
	// the saved sections too.
	srv, _ := servertest.Start(t, cfg)
	srv.SetLoadedBlock(5, 64, 5, registry.BlockStates["minecraft:torch"])
	activeState, _ := registry.FindState("minecraft:trial_spawner", map[string]string{"trial_spawner_state": "active"})
	inactiveState, _ := registry.FindState("minecraft:trial_spawner", map[string]string{"trial_spawner_state": "inactive"})
	srv.SetLoadedBlock(10, 64, 3, activeState)
	srv.SetLoadedBlock(15, 64, 3, inactiveState)
	srv.Shutdown()

	// The chunk file's block entities go in by hand, as a vanilla world would
	// have saved them.
	path := servertest.ChunkRegionPath(cfg.LevelName, world.ChunkPos{})
	servertest.EditChunkRecord(t, path, world.ChunkPos{}, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress chunk: %v", err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		_, tag, err := nbt.Decode(raw)
		if err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		root, ok := tag.(nbt.Compound)
		if !ok {
			t.Fatalf("chunk root = %#v, want a compound", tag)
		}
		root["block_entities"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			chunkMetadata(signEntity("minecraft:sign", 1)),
			chunkMetadata(signEntity("minecraft:hanging_sign", 2)),
			chunkMetadata(spawnerEntity(3)),
			chunkMetadata(bannerEntity(4)),
			chunkMetadata(skullEntity(5)),
			chunkMetadata(chestEntity(6)),
			chunkMetadata(conduitEntity(7)),
			chunkMetadata(creakingHeartEntity(8)),
			chunkMetadata(vaultEntity(9)),
			chunkMetadata(trialSpawnerEntity(10)),
			chunkMetadata(campfireEntity(11)),
			chunkMetadata(brushableEntity(12)),
			chunkMetadata(freshBrushableEntity(13)),
			chunkMetadata(decoratedPotEntity(14)),
			chunkMetadata(inactiveTrialSpawnerEntity(15)),
			chunkMetadata(targetlessConduitEntity(0)),
			chunkMetadata(emptyCampfireEntity(11, 4)),
			chunkMetadata(bareShelfEntity(12, 4)),
			chunkMetadata(positionlessEntity()),
		}}
		out, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(out); err != nil {
			t.Fatalf("compress chunk: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("close compressor: %v", err)
		}
		newRecord := make([]byte, 5+buf.Len())
		binary.BigEndian.PutUint32(newRecord, uint32(1+buf.Len()))
		newRecord[4] = record[4]
		copy(newRecord[5:], buf.Bytes())
		return newRecord
	})

	// The world opened again reads the block entities back from the file and
	// sends them with the chunk.
	restarted := *cfg
	_, connect := servertest.Start(t, &restarted)
	c := connect()
	c.Login("Reader")
	c.Configure()
	entries := chunkBlockEntities(t, servertest.ChunkAt(t, c, 0, 0))

	// The sign's text is what a blank sign was missing; its id and position
	// stay behind.
	sign := blockEntityAt(t, entries, 1, 64, 3)
	if sign.typeID != world.BlockEntityTypeID("minecraft:sign") {
		t.Errorf("sign entry type = %d, want %d", sign.typeID, world.BlockEntityTypeID("minecraft:sign"))
	}
	if got := signLine(t, sign.tag, "front_text", 0); got != "Hello" {
		t.Errorf("sign front line 0 = %q, want the text the file held", got)
	}
	assertNoMetadata(t, "sign", sign.tag)

	hanging := blockEntityAt(t, entries, 2, 64, 3)
	if hanging.typeID != world.BlockEntityTypeID("minecraft:hanging_sign") {
		t.Errorf("hanging sign entry type = %d, want %d", hanging.typeID, world.BlockEntityTypeID("minecraft:hanging_sign"))
	}
	if got := signLine(t, hanging.tag, "front_text", 0); got != "Hang" {
		t.Errorf("hanging sign front line 0 = %q, want the text the file held", got)
	}
	assertNoMetadata(t, "hanging sign", hanging.tag)

	// A spawner shows its next spawn; the potentials the update tag drops are
	// gone.
	spawner := blockEntityAt(t, entries, 3, 64, 3)
	if spawner.typeID != world.BlockEntityTypeID("minecraft:mob_spawner") {
		t.Errorf("spawner entry type = %d, want %d", spawner.typeID, world.BlockEntityTypeID("minecraft:mob_spawner"))
	}
	data, ok := nbt.Get[nbt.Compound](spawner.tag, "SpawnData")
	if !ok {
		t.Fatalf("spawner entry has no SpawnData: %v", spawner.tag)
	}
	entity, _ := nbt.Get[nbt.Compound](data, "entity")
	if id, _ := nbt.Get[nbt.String](entity, "id"); id != "minecraft:cow" {
		t.Errorf("spawner's next spawn = %q, want the cow", id)
	}
	if _, ok := spawner.tag["SpawnPotentials"]; ok {
		t.Errorf("spawner entry carries SpawnPotentials: %v", spawner.tag)
	}

	// A carried banner keeps its patterns.
	banner := blockEntityAt(t, entries, 4, 64, 3)
	if banner.typeID != world.BlockEntityTypeID("minecraft:banner") {
		t.Errorf("banner entry type = %d, want %d", banner.typeID, world.BlockEntityTypeID("minecraft:banner"))
	}
	patterns, ok := nbt.Get[nbt.List](banner.tag, "patterns")
	if !ok || len(patterns.Items) != 1 {
		t.Fatalf("banner entry patterns = %v, want the one the file held", banner.tag["patterns"])
	}
	assertNoMetadata(t, "banner", banner.tag)
	// The banner is the one table type whose update tag keeps components.
	if _, ok := banner.tag["components"]; !ok {
		t.Errorf("banner entry dropped components, which its update tag keeps: %v", banner.tag)
	}

	// A carried skull keeps its note block sound.
	skull := blockEntityAt(t, entries, 5, 64, 3)
	if skull.typeID != world.BlockEntityTypeID("minecraft:skull") {
		t.Errorf("skull entry type = %d, want %d", skull.typeID, world.BlockEntityTypeID("minecraft:skull"))
	}
	if sound, _ := nbt.Get[nbt.String](skull.tag, "note_block_sound"); sound != "minecraft:block.note_block.pling" {
		t.Errorf("skull note_block_sound = %q, want the file's", sound)
	}
	assertNoMetadata(t, "skull", skull.tag)
	if _, ok := skull.tag["components"]; ok {
		t.Errorf("skull entry carries components, which its update tag drops: %v", skull.tag)
	}

	// The chest is listed -- the client has to know it is there -- with no
	// tag: vanilla's update tag for a container is empty, its contents
	// reaching a client through the window it opens.
	chest := blockEntityAt(t, entries, 6, 64, 3)
	if chest.typeID != world.BlockEntityTypeID("minecraft:chest") {
		t.Errorf("chest entry type = %d, want %d", chest.typeID, world.BlockEntityTypeID("minecraft:chest"))
	}
	if chest.tag != nil {
		t.Errorf("chest entry carries a tag, want none: %v", chest.tag)
	}

	// A conduit sends the target its beam attacks; a creaking heart sends its
	// creaking. Neither carries any chunk metadata.
	conduit := blockEntityAt(t, entries, 7, 64, 3)
	if target, ok := nbt.Get[nbt.IntArray](conduit.tag, "Target"); !ok || len(target) != 4 {
		t.Errorf("conduit entry Target = %v, want the four ints the file held", conduit.tag["Target"])
	}
	assertOnlyKeys(t, "conduit", conduit.tag, "Target")

	creaking := blockEntityAt(t, entries, 8, 64, 3)
	if _, ok := nbt.Get[nbt.IntArray](creaking.tag, "creaking"); !ok {
		t.Errorf("creaking heart entry has no creaking: %v", creaking.tag)
	}
	assertOnlyKeys(t, "creaking heart", creaking.tag, "creaking")

	// A conduit with no target sends nothing, listed with one end byte.
	bareConduit := blockEntityAt(t, entries, 0, 64, 3)
	if bareConduit.tag != nil {
		t.Errorf("targetless conduit entry carries a tag, want none: %v", bareConduit.tag)
	}

	// A never-brushed brushable block has an empty update tag too.
	freshBrushable := blockEntityAt(t, entries, 13, 64, 3)
	if freshBrushable.tag != nil {
		t.Errorf("fresh brushable entry carries a tag, want none: %v", freshBrushable.tag)
	}

	// A vault sends only its shared data; the config and the server's own
	// data stay behind.
	vault := blockEntityAt(t, entries, 9, 64, 3)
	assertOnlyKeys(t, "vault", vault.tag, "shared_data")
	if _, ok := nbt.Get[nbt.Compound](vault.tag, "shared_data"); !ok {
		t.Errorf("vault entry shared_data = %v, want the file's compound", vault.tag)
	}

	// A trial spawner sends its next spawn and, while its block is ACTIVE,
	// when that spawn is due - no config, no player or mob lists. An inactive
	// spawner drops the stored tick rather than send it.
	spawnerTrial := blockEntityAt(t, entries, 10, 64, 3)
	assertOnlyKeys(t, "trial spawner", spawnerTrial.tag, "spawn_data", "next_mob_spawns_at")
	if at, _ := nbt.Get[nbt.Long](spawnerTrial.tag, "next_mob_spawns_at"); at != 1234 {
		t.Errorf("trial spawner next_mob_spawns_at = %d, want 1234", at)
	}

	inactiveTrial := blockEntityAt(t, entries, 15, 64, 3)
	assertOnlyKeys(t, "inactive trial spawner", inactiveTrial.tag, "spawn_data")

	// A campfire sends the items on it, not the cooking timers, always naming
	// the list even when the block holds none.
	campfire := blockEntityAt(t, entries, 11, 64, 3)
	assertOnlyKeys(t, "campfire", campfire.tag, "Items")
	if items, ok := nbt.Get[nbt.List](campfire.tag, "Items"); !ok || len(items.Items) != 1 {
		t.Errorf("campfire entry Items = %v, want the one the file held", campfire.tag["Items"])
	}

	emptyFire := blockEntityAt(t, entries, 11, 64, 4)
	assertOnlyKeys(t, "empty campfire", emptyFire.tag, "Items")
	if items, ok := nbt.Get[nbt.List](emptyFire.tag, "Items"); !ok || len(items.Items) != 0 {
		t.Errorf("empty campfire entry Items = %v, want an empty list", emptyFire.tag["Items"])
	}

	// A shelf always names its items - an empty list when the file held none
	// - and whether they hug the bottom.
	shelf := blockEntityAt(t, entries, 12, 64, 4)
	assertOnlyKeys(t, "shelf", shelf.tag, "Items", "align_items_to_bottom")
	if items, ok := nbt.Get[nbt.List](shelf.tag, "Items"); !ok || len(items.Items) != 0 {
		t.Errorf("shelf entry Items = %v, want an empty list", shelf.tag["Items"])
	}
	if align, ok := nbt.Get[nbt.Byte](shelf.tag, "align_items_to_bottom"); !ok || align != 1 {
		t.Errorf("shelf entry align_items_to_bottom = %v, want the file's 1", shelf.tag["align_items_to_bottom"])
	}

	// A brushable block sends the direction it was brushed from and the item
	// it hides, not the loot table that item came from.
	brushable := blockEntityAt(t, entries, 12, 64, 3)
	assertOnlyKeys(t, "brushable", brushable.tag, "hit_direction", "item")
	if _, ok := brushable.tag["LootTable"]; ok {
		t.Errorf("brushable entry carries the loot table: %v", brushable.tag)
	}

	// A decorated pot is a table type: the chunk metadata goes, its own
	// fields stay.
	pot := blockEntityAt(t, entries, 14, 64, 3)
	assertNoMetadata(t, "decorated pot", pot.tag)
	if _, ok := pot.tag["item"]; !ok {
		t.Errorf("decorated pot entry dropped its item: %v", pot.tag)
	}

	// A carried entry with no position is left out, not listed at (0,0,0).
	for _, e := range entries {
		if e.packedXZ == 0 && e.y == 0 {
			t.Errorf("a carried entry with no position was listed at (0,0,0): %v", e)
		}
	}
}

// assertOnlyKeys fails unless tag carries exactly the named keys.
func assertOnlyKeys(t *testing.T, what string, tag nbt.Compound, keys ...string) {
	t.Helper()
	if len(tag) != len(keys) {
		t.Fatalf("%s update tag = %v, want only %v", what, tag, keys)
	}
	for _, key := range keys {
		if _, ok := tag[key]; !ok {
			t.Errorf("%s update tag is missing %q: %v", what, key, tag)
		}
	}
}

// chunkMetadata adds keepPacked and components, the two keys a vanilla chunk
// file writes on every block entity.
func chunkMetadata(entry nbt.Compound) nbt.Compound {
	entry["keepPacked"] = nbt.Byte(0)
	entry["components"] = nbt.Compound{}
	return entry
}

// signEntity is one sign-like block entity entry as a chunk file saves it.
func signEntity(id string, x int32) nbt.Compound {
	front := "Hello"
	if id == "minecraft:hanging_sign" {
		front = "Hang"
	}
	return nbt.Compound{
		"id":       nbt.String(id),
		"x":        nbt.Int(x),
		"y":        nbt.Int(64),
		"z":        nbt.Int(3),
		"is_waxed": nbt.Byte(0),
		"front_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String(front), nbt.String(""), nbt.String(""), nbt.String(""),
			}},
			"color":            nbt.String("black"),
			"has_glowing_text": nbt.Byte(0),
		},
		"back_text": nbt.Compound{
			"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
				nbt.String(""), nbt.String(""), nbt.String(""), nbt.String(""),
			}},
			"color":            nbt.String("black"),
			"has_glowing_text": nbt.Byte(0),
		},
	}
}

// spawnerEntity is a spawner entry whose next spawn is a cow.
func spawnerEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:mob_spawner"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"Delay": nbt.Short(20), "MinSpawnDelay": nbt.Short(200), "MaxSpawnDelay": nbt.Short(800),
		"SpawnCount": nbt.Short(4), "MaxNearbyEntities": nbt.Short(6),
		"RequiredPlayerRange": nbt.Short(16), "SpawnRange": nbt.Short(4),
		"SpawnData":       nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cow")}},
		"SpawnPotentials": nbt.List{},
	}
}

// bannerEntity is a carried banner entry with one pattern.
func bannerEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:banner"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"patterns": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"color": nbt.String("red"), "pattern": nbt.String("minecraft:cross")},
		}},
	}
}

// skullEntity is a carried skull entry with a note block sound.
func skullEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:skull"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"note_block_sound": nbt.String("minecraft:block.note_block.pling"),
	}
}

// chestEntity is a chest entry holding two diamonds.
func chestEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(3), "id": nbt.String("minecraft:diamond"), "count": nbt.Int(2)},
		}},
	}
}

// conduitEntity is a carried conduit holding a target, which its update tag
// sends.
func conduitEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:conduit"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"Target": nbt.IntArray{1, 2, 3, 4},
	}
}

// targetlessConduitEntity is a carried conduit with no target, whose update
// tag is empty.
func targetlessConduitEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:conduit"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
	}
}

// creakingHeartEntity is a carried creaking heart holding its creaking, which
// its update tag sends.
func creakingHeartEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:creaking_heart"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"creaking": nbt.IntArray{5, 6, 7, 8},
	}
}

// vaultEntity is a carried vault holding its config, the server's own data
// and the shared data the client does receive.
func vaultEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:vault"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"config":      nbt.Compound{"loot_table": nbt.String("minecraft:chests/trial_chambers/reward")},
		"server_data": nbt.Compound{"items_to_eject": nbt.List{}},
		"shared_data": nbt.Compound{"display_item": nbt.Compound{"id": nbt.String("minecraft:diamond")}},
	}
}

// trialSpawnerEntity is a carried trial spawner that is active, its next
// spawn due at tick 1234.
func trialSpawnerEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:trial_spawner"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"registered_players":     nbt.List{},
		"current_mobs":           nbt.List{},
		"cooldown_ends_at":       nbt.Long(0),
		"next_mob_spawns_at":     nbt.Long(1234),
		"total_mobs_spawned":     nbt.Int(3),
		"spawn_data":             nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:zombie")}},
		"ejecting_loot_table":    nbt.String("minecraft:empty"),
		"normal_config":          nbt.Compound{"spawn_range": nbt.Int(4)},
		"ominous_config":         nbt.Compound{"spawn_range": nbt.Int(4)},
		"target_cooldown_length": nbt.Int(36000),
		"required_player_range":  nbt.Int(14),
	}
}

// inactiveTrialSpawnerEntity is a carried trial spawner whose block state is
// inactive but whose stored next spawn tick is still set, which its update
// tag drops.
func inactiveTrialSpawnerEntity(x int32) nbt.Compound {
	entry := trialSpawnerEntity(x)
	entry["next_mob_spawns_at"] = nbt.Long(4321)
	return entry
}

// campfireEntity is a carried campfire holding one item, with its cooking
// timers, which its update tag drops.
func campfireEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:campfire"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(2), "id": nbt.String("minecraft:porkchop"), "count": nbt.Int(1)},
		}},
		"CookingTimes":      nbt.IntArray{100, 0, 0, 0},
		"CookingTotalTimes": nbt.IntArray{600, 0, 0, 0},
	}
}

// emptyCampfireEntity is a carried campfire with no Items key at all, which
// its update tag still names as an empty list.
func emptyCampfireEntity(x, z int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:campfire"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(z),
		"CookingTimes":      nbt.IntArray{0, 0, 0, 0},
		"CookingTotalTimes": nbt.IntArray{0, 0, 0, 0},
	}
}

// bareShelfEntity is a carried shelf with no Items key at all, which its
// update tag still names as an empty list, and with its items aligned to the
// bottom.
func bareShelfEntity(x, z int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:shelf"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(z),
		"align_items_to_bottom": nbt.Byte(1),
	}
}

// brushableEntity is a carried suspicious sand that hides an item and the
// direction it was brushed from, with a loot table its update tag drops.
func brushableEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:brushable_block"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"LootTable":     nbt.String("minecraft:archaeology/desert_pyramid"),
		"LootTableSeed": nbt.Long(42),
		"item":          nbt.Compound{"id": nbt.String("minecraft:archer_pottery_sherd"), "count": nbt.Int(1)},
		"hit_direction": nbt.Byte(3),
	}
}

// freshBrushableEntity is a carried suspicious gravel that was never brushed,
// so its update tag is empty.
func freshBrushableEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:brushable_block"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"LootTable":     nbt.String("minecraft:archaeology/desert_well"),
		"LootTableSeed": nbt.Long(7),
	}
}

// decoratedPotEntity is a carried decorated pot, a table type whose own item
// travels but whose chunk metadata does not.
func decoratedPotEntity(x int32) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:decorated_pot"), "x": nbt.Int(x), "y": nbt.Int(64), "z": nbt.Int(3),
		"item":   nbt.Compound{"id": nbt.String("minecraft:brick"), "count": nbt.Int(1)},
		"sherds": nbt.List{},
	}
}

// positionlessEntity is a carried beehive with no position, which the packet
// leaves out rather than listing at (0,0,0).
func positionlessEntity() nbt.Compound {
	return nbt.Compound{
		"id":        nbt.String("minecraft:beehive"),
		"FlowerPos": nbt.Compound{"X": nbt.Int(0), "Y": nbt.Int(64), "Z": nbt.Int(0)},
	}
}

// signLine reads one message out of a sign entry's side.
func signLine(t *testing.T, tag nbt.Compound, side string, line int) string {
	t.Helper()
	text, ok := nbt.Get[nbt.Compound](tag, side)
	if !ok {
		t.Fatalf("block entity has no %s: %v", side, tag)
	}
	messages, ok := nbt.Get[nbt.List](text, "messages")
	if !ok || len(messages.Items) <= line {
		t.Fatalf("%s.messages = %v, want at least %d lines", side, text["messages"], line+1)
	}
	s, _ := messages.Items[line].(nbt.String)
	return string(s)
}

// assertNoMetadata checks an entry's tag carries neither the id, the position
// nor the chunk format's keepPacked flag, none of which an update tag sends.
func assertNoMetadata(t *testing.T, what string, tag nbt.Compound) {
	t.Helper()
	for _, key := range []string{"id", "x", "y", "z", "keepPacked"} {
		if _, ok := tag[key]; ok {
			t.Errorf("%s update tag carries %q: %v", what, key, tag)
		}
	}
}
