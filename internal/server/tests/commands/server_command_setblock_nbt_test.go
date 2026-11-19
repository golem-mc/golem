package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// expectBlockEntityData reads the packet the server broadcasts a block
// entity's data with, stepping over the noise a command's run and a ticking
// world fill the stream with, and fails when none arrives within ten seconds.
func expectBlockEntityData(t *testing.T, c *servertest.Client) []byte {
	t.Helper()
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayBlockEntityData:
			return data
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayWorldEvent,
			protocol.IDPlaySoundEffect, protocol.IDPlayUpdateLight,
			protocol.IDPlayUpdateTime, protocol.IDPlayEntityMetadata,
			protocol.IDPlaySystemChat, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySetHealth, protocol.IDPlayKeepAlive,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for block entity data", id)
		}
	}
	t.Fatal("block entity data did not arrive within 10s")
	return nil
}

// TestSetblockGivesTheBlockItsTag checks the block entity tag a block
// argument carries: a tag for a block entity this server does not model is
// carried by the chunk whole and told to the clients as the update tag the
// packet carries, a container's tag stands its block entity up holding what
// the tag named but is told to nobody, and a sign goes out in the vanilla
// sign entity's own shape, whichever keys its tag named.
func TestSetblockGivesTheBlockItsTag(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /setblock is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()
	// Flush each placement only after its command has loaded the block entity tag.
	servertest.HoldTicks(t, srv)

	// A trial spawner in the cooldown state, holding the one field a
	// command can name on it: the block is placed, the chunk carries the
	// tag whole, and the clients get the carried compound without the
	// position the packet does not carry; cooldown_ends_at is saved but not in the update tag.
	cooldown, ok := registry.FindState("minecraft:trial_spawner", map[string]string{"trial_spawner_state": "cooldown"})
	if !ok {
		t.Fatal("the registry has no cooling trial spawner")
	}
	c.ChatCommand("setblock 2 64 3 minecraft:trial_spawner[trial_spawner_state=cooldown]{cooldown_ends_at:900000000L}")
	data := c.ExpectWithinTicks(srv, 1, protocol.IDPlayBlockEntityData)
	r := protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 2 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (2, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:trial_spawner") {
		t.Errorf("block entity data type = %d, want %d (the trial spawner)", got, world.BlockEntityTypeID("minecraft:trial_spawner"))
	}
	tag, _, err := nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ := tag.(nbt.Compound)
	if _, ok := compound["cooldown_ends_at"]; ok {
		t.Errorf("the packet carries cooldown_ends_at, which the trial spawner's update tag leaves out: %v", compound)
	}
	if _, ok := compound["id"]; ok {
		t.Errorf("the packet carries an id, which vanilla's update tag leaves out: %v", compound)
	}
	if _, ok := compound["x"]; ok {
		t.Errorf("the packet carries a position: %v", compound)
	}
	c.Drain()

	if got := srv.BlockAt(2, 64, 3); got != cooldown {
		t.Errorf("block at (2, 64, 3) = %d, want %d (cooling trial spawner)", got, cooldown)
	}
	carried, ok := srv.BlockEntityTagAt(2, 64, 3)
	if !ok {
		t.Fatal("the spawner's tag was not carried")
	}
	if got, _ := nbt.Get[nbt.Long](carried, "cooldown_ends_at"); got != 900000000 {
		t.Errorf("carried cooldown_ends_at = %d, want 900000000", got)
	}

	// A chest with something in its third slot: the tag lands on the block
	// entity the server models, whose window would show it, and nothing
	// goes to the clients, which hear of a container through the window
	// they open.
	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the registry has no chest")
	}
	c.ChatCommand(`setblock 3 64 3 minecraft:chest{Items:[{Slot:3b,id:"minecraft:diamond",count:2}]}`)
	var told bool
	c.ReadTicks(srv, 1, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayBlockEntityData {
			told = true
		}
		return false
	})
	if told {
		t.Error("the fresh chest told the clients its block entity data")
	}

	if got := srv.BlockAt(3, 64, 3); got != chest {
		t.Errorf("block at (3, 64, 3) = %d, want %d (chest)", got, chest)
	}
	stacks := srv.InventoryAt(3, 64, 3)
	if len(stacks) != 27 {
		t.Fatalf("the chest holds %d slots, want 27", len(stacks))
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	if got := stacks[3]; got.ItemID != diamond || got.Count != 2 {
		t.Errorf("chest slot 3 = %+v, want two diamonds", got)
	}

	// The sign put beside it is what proves the chest sent nothing: the
	// next block entity data is the sign's.
	c.ChatCommand(`setblock 4 64 3 minecraft:oak_sign{is_waxed:1b}`)
	data = c.ExpectWithinTicks(srv, 1, protocol.IDPlayBlockEntityData)
	r = protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 4 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (4, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:sign") {
		t.Errorf("block entity data type = %d, want %d (the sign)", got, world.BlockEntityTypeID("minecraft:sign"))
	}
	tag, _, err = nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ = tag.(nbt.Compound)
	if got, _ := nbt.Get[nbt.Byte](compound, "is_waxed"); got != 1 {
		t.Errorf("the packet's is_waxed = %d, want 1", got)
	}
	expectNormalSign(t, compound)
	c.Drain()

	// A sign with a key vanilla's sign has no field for: the packet is the
	// sign's own save, and the key has nowhere to go.
	c.ChatCommand(`setblock 7 64 3 minecraft:oak_sign{foo:1b}`)
	data = c.ExpectWithinTicks(srv, 1, protocol.IDPlayBlockEntityData)
	r = protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 7 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (7, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:sign") {
		t.Errorf("block entity data type = %d, want %d (the sign)", got, world.BlockEntityTypeID("minecraft:sign"))
	}
	tag, _, err = nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ = tag.(nbt.Compound)
	if _, ok := compound["foo"]; ok {
		t.Errorf("the packet carries a key vanilla's sign has no field for: %v", compound)
	}
	if got, _ := nbt.Get[nbt.Byte](compound, "is_waxed"); got != 0 {
		t.Errorf("the packet's is_waxed = %d, want 0", got)
	}
	expectNormalSign(t, compound)
	c.Drain()

	// A spawner the server models: its own compound goes out, the fields
	// the tag named among them.
	c.ChatCommand("setblock 5 64 3 minecraft:spawner{MaxNearbyEntities:9}")
	data = c.ExpectWithinTicks(srv, 1, protocol.IDPlayBlockEntityData)
	r = protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 5 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (5, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:mob_spawner") {
		t.Errorf("block entity data type = %d, want %d (the spawner)", got, world.BlockEntityTypeID("minecraft:mob_spawner"))
	}
	tag, _, err = nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ = tag.(nbt.Compound)
	if got, _ := nbt.Get[nbt.Short](compound, "MaxNearbyEntities"); got != 9 {
		t.Errorf("the packet's MaxNearbyEntities = %d, want 9", got)
	}
	if _, ok := compound["SpawnPotentials"]; ok {
		t.Errorf("the packet carries the potentials, which vanilla's update tag drops: %v", compound)
	}
	if _, ok := compound["id"]; ok {
		t.Errorf("the packet carries an id, which vanilla's update tag leaves out: %v", compound)
	}
	c.Drain()
	c.Disconnect()
}

// TestSetblockSpawnerKeepsItsSpawnData pins the issue's own scenario on
// /setblock: the SpawnData a command names on a spawner is what the block
// entity data packet carries and what the block entity holds.
func TestSetblockSpawnerKeepsItsSpawnData(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	c.ChatCommand(`setblock 2 64 3 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	data := expectBlockEntityData(t, c)
	r := protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 2 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (2, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:mob_spawner") {
		t.Errorf("block entity data type = %d, want %d (the spawner)", got, world.BlockEntityTypeID("minecraft:mob_spawner"))
	}
	tag, _, err := nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ := tag.(nbt.Compound)
	if got := spawnDataID(compound); got != "minecraft:skeleton" {
		t.Errorf("the packet's SpawnData names %q, want minecraft:skeleton", got)
	}
	held, ok := srv.BlockEntityTagAt(2, 64, 3)
	if !ok {
		t.Fatal("the spawner carries no block entity")
	}
	if got := spawnDataID(held); got != "minecraft:skeleton" {
		t.Errorf("the spawner holds %q, want minecraft:skeleton", got)
	}
	c.Disconnect()
}

// expectNormalSign checks the update tag a sign goes out with is the
// vanilla sign entity's own save: is_waxed and a full text on each side -
// four messages, a colour and a glow flag each - and nothing else, the
// vanillas update tag being saveCustomOnly.
// vanilla: SignBlockEntity.saveAdditional
func expectNormalSign(t *testing.T, compound nbt.Compound) {
	t.Helper()
	if len(compound) != 3 {
		t.Fatalf("the sign's update tag = %v, want is_waxed, front_text and back_text", compound)
	}
	for _, side := range []string{"front_text", "back_text"} {
		text, ok := nbt.Get[nbt.Compound](compound, side)
		if !ok {
			t.Errorf("the sign's %s is missing: %v", side, compound)
			continue
		}
		messages, ok := nbt.Get[nbt.List](text, "messages")
		if !ok || len(messages.Items) != 4 {
			t.Errorf("the sign's %s.messages = %v, want four lines", side, text)
		}
		if got, _ := nbt.Get[nbt.String](text, "color"); got != "black" {
			t.Errorf("the sign's %s.color = %q, want black, the fresh sign's own", side, got)
		}
		if got, _ := nbt.Get[nbt.Byte](text, "has_glowing_text"); got != 0 {
			t.Errorf("the sign's %s.has_glowing_text = %d, want 0", side, got)
		}
	}
}

// TestSetblockTellsTheClientsWhenARetagChangesTheTag checks the second
// half of a setblock that changes nothing but the tag: the block that
// already stood as asked takes the new tag when it changes what its block
// entity holds, which is when the placement answers true, and takes it as a
// failure when it does not. The container hears nothing either way, the
// spawner the server models gets its packet both ways, and the carried ones
// move their update tag.
func TestSetblockTellsTheClientsWhenARetagChangesTheTag(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// A chest that keeps its tag: the run answers the vanilla failure, and
	// the next block entity data the client hears of is not the chest's,
	// which is silent — the spawner beside it speaks first.
	c.ChatCommand(`setblock 2 64 3 minecraft:chest{Items:[{Slot:1b,id:"minecraft:diamond",count:1}]}`)
	c.Drain()
	diamond := srv.InventoryAt(2, 64, 3)[1]
	c.ChatCommand(`setblock 2 64 3 minecraft:chest{Items:[{Slot:1b,id:"minecraft:diamond",count:1}]}`)
	servertest.AwaitCommandFailure(t, c, "commands.setblock.failed")
	c.Drain()
	if got := srv.InventoryAt(2, 64, 3)[1]; got.ItemID != diamond.ItemID || got.Count != diamond.Count {
		t.Errorf("the refused retag moved the chest: %+v, want %+v", got, diamond)
	}

	// A chest whose tag changed: the run answers success and the chest
	// holds the new thing, silent to the clients as ever.
	c.ChatCommand(`setblock 2 64 3 minecraft:chest{Items:[{Slot:1b,id:"minecraft:emerald",count:5}]}`)
	var told bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayBlockEntityData {
			told = true
		}
	})
	if told {
		t.Error("the retagged chest told the clients its block entity data")
	}
	emerald, ok := registry.ItemID("minecraft:emerald")
	if !ok {
		t.Fatal("the registry has no emerald")
	}
	if got := srv.InventoryAt(2, 64, 3)[1]; got.ItemID != emerald || got.Count != 5 {
		t.Errorf("the retagged chest slot 1 = %+v, want five emeralds", got)
	}

	// A spawner whose tag changed: the run answers success and the
	// clients get the spawner's own compound with the new field. The
	// chest before it said nothing, so the first packet is the spawner's.
	c.ChatCommand("setblock 3 64 3 minecraft:spawner{MaxNearbyEntities:4}")
	data := expectBlockEntityData(t, c)
	r := protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 3 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (3, 64, 3)", x, y, z)
	}
	c.ChatCommand("setblock 3 64 3 minecraft:spawner{MaxNearbyEntities:9}")
	data = expectBlockEntityData(t, c)
	r = protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 3 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (3, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:mob_spawner") {
		t.Errorf("block entity data type = %d, want %d (the spawner)", got, world.BlockEntityTypeID("minecraft:mob_spawner"))
	}
	rest := r.Rest()
	tag, _, err := nbt.DecodeNetwork(rest)
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ := tag.(nbt.Compound)
	if got, _ := nbt.Get[nbt.Short](compound, "MaxNearbyEntities"); got != 9 {
		t.Errorf("the packet's MaxNearbyEntities = %d, want 9", got)
	}
	c.Drain()

	// A carried one whose tag changed: the update tag moves to the new
	// fields, again without the position.
	c.ChatCommand("setblock 4 64 3 minecraft:trial_spawner{cooldown_ends_at:1L}")
	expectBlockEntityData(t, c)
	c.Drain()
	c.ChatCommand("setblock 4 64 3 minecraft:trial_spawner{cooldown_ends_at:2L}")
	data = expectBlockEntityData(t, c)
	r = protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 4 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (4, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:trial_spawner") {
		t.Errorf("block entity data type = %d, want %d (the trial spawner)", got, world.BlockEntityTypeID("minecraft:trial_spawner"))
	}
	tag, _, err = nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ = tag.(nbt.Compound)
	if _, ok := compound["cooldown_ends_at"]; ok {
		t.Errorf("the packet carries cooldown_ends_at, which the trial spawner's update tag leaves out: %v", compound)
	}
	if saved, _ := srv.BlockEntityTagAt(4, 64, 3); saved != nil {
		if got, _ := nbt.Get[nbt.Long](saved, "cooldown_ends_at"); got != 2 {
			t.Errorf("saved cooldown_ends_at = %d, want 2", got)
		}
	} else {
		t.Error("the retagged spawner's tag was not carried")
	}
	if _, ok := compound["x"]; ok {
		t.Errorf("the packet carries a position: %v", compound)
	}
	c.Drain()
	c.Disconnect()
}

// The silent kinds go beside the sign that follows them, at (4, 64, 2), so
// the sign's block entity data is the first the client hears.
const setblockSilentAt = "setblock 4 64 2 "

// TestSetblockTellsTheSilentTypesNothing checks the block entity types
// whose vanilla class has no update packet are told to nobody: the
// trapped chest - a type of its own, not the chest the silent list once
// named - the bell and the jukebox take their tags in, and the next block
// entity data the client hears is the sign placed beside them.
func TestSetblockTellsTheSilentTypesNothing(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	for _, block := range []string{
		`minecraft:trapped_chest{Items:[{Slot:0b,id:"minecraft:stone",count:1b}]}`,
		`minecraft:bell{foo:1b}`,
		`minecraft:jukebox{RecordItem:{id:"minecraft:music_disc_cat",count:1b}}`,
	} {
		c.ChatCommand(setblockSilentAt + block)
		var told bool
		c.SettleAndCollect(func(id int32, _ []byte) {
			if id == protocol.IDPlayBlockEntityData {
				told = true
			}
		})
		if told {
			t.Errorf("%s told the clients its block entity data", block)
		}
	}

	c.ChatCommand(`setblock 4 64 3 minecraft:oak_sign{is_waxed:1b}`)
	data := expectBlockEntityData(t, c)
	r := protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 4 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (4, 64, 3)", x, y, z)
	}
	c.Drain()
	c.Disconnect()
}

// TestSetblockModeStandsAfterTheTag checks the mode a setblock names landing
// after the tag, however many words the tag's braces span: a keep with a tag
// spares the block that is already there, and one without the braces still
// reads the mode in the word after the block.
func TestSetblockModeStandsAfterTheTag(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	srv.SetLoadedBlock(2, 64, 3, stone)
	c.ChatCommand("setblock 2 64 3 barrel{ Items: [ ] } keep")
	c.Drain()
	if got := srv.BlockAt(2, 64, 3); got != stone {
		t.Errorf("block at (2, 64, 3) = %d, want %d (keep spared the stone)", got, stone)
	}
	if _, ok := srv.BlockEntityTagAt(2, 64, 3); ok {
		t.Error("the spared stone grew a block entity")
	}

	// The same span with the replace mode, which puts the block in place.
	// The barrel's tag lands on the block entity it stands up, and the
	// barrel is told to nobody.
	c.ChatCommand("setblock 2 64 3 barrel{ Items: [ ] } replace")
	var told bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayBlockEntityData {
			told = true
		}
	})
	if told {
		t.Error("the barrel told the clients its block entity data")
	}
	barrel, ok := registry.FindState("minecraft:barrel", nil)
	if !ok {
		t.Fatal("the registry has no barrel")
	}
	if got := srv.BlockAt(2, 64, 3); got != barrel {
		t.Errorf("block at (2, 64, 3) = %d, want %d (barrel)", got, barrel)
	}
	if _, ok := srv.BlockEntityTagAt(2, 64, 3); !ok {
		t.Error("the barrel's tag did not land")
	}
	// The sign put beside it is what proves the barrel sent nothing: the
	// next block entity data is the sign's.
	c.ChatCommand("setblock 6 64 3 minecraft:oak_sign{is_waxed:1b}")
	data := expectBlockEntityData(t, c)
	r := protocol.NewReader(data)
	if x, y, z := r.ReadPosition(); x != 6 || y != 64 || z != 3 {
		t.Errorf("block entity data position = (%d, %d, %d), want (6, 64, 3)", x, y, z)
	}
	if got := r.ReadVarInt(); got != world.BlockEntityTypeID("minecraft:sign") {
		t.Errorf("block entity data type = %d, want %d (the sign)", got, world.BlockEntityTypeID("minecraft:sign"))
	}
	c.Drain()
	c.Disconnect()
}

// TestSetblockBadBlockArgNamesWhatIsWrong checks the parse error a block
// argument the vanilla parser would refuse is answered with: the property
// faults carry the vanilla keys and the byte they stopped at, and a tag
// that will not read carries the SNBT reader's key — or, for a shape the
// client has no key for, the words the reader itself said. Each is
// followed by the context line pointing into the command.
func TestSetblockBadBlockArgNamesWhatIsWrong(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The block argument is the fourth word, so every cursor inside it is
	// its offset in the command. The tag rows are the answers a vanilla
	// server gives the same commands, whose grammar reports the first
	// expectation among the entries at the furthest cursor.
	const prefix = len("setblock 2 64 3 ")
	chestOffset := prefix + len("minecraft:chest")
	for _, tc := range []struct {
		name   string
		block  string
		key    string
		with   []string
		cursor int
	}{
		{"property the block does not have", "minecraft:stone[facing=north]",
			"argument.block.property.unknown", []string{"minecraft:stone", "facing"}, prefix + len("minecraft:stone") + 1},
		{"value the property does not take", "minecraft:oak_leaves[distance=9]",
			"argument.block.property.invalid", []string{"minecraft:oak_leaves", "9", "distance"}, prefix + len("minecraft:oak_leaves[distance=")},
		{"property set twice", "minecraft:oak_leaves[distance=1,distance=2]",
			"argument.block.property.duplicate", []string{"distance", "minecraft:oak_leaves"}, prefix + len("minecraft:oak_leaves[distance=1,")},
		{"properties that never close", "minecraft:oak_leaves[distance=1",
			"argument.block.property.unclosed", nil, prefix + len("minecraft:oak_leaves[distance=1")},
		{"hexadecimal numeral with no digits", "minecraft:chest{a:0x}",
			"snbt.parser.expected_hex_numeral", nil, chestOffset + 5},
		{"value that never came", "minecraft:chest{a:1q}",
			"argument.literal.incorrect", []string{"."}, chestOffset + 4},
		{"second entry with no comma between", "minecraft:chest{a:1 b:2}",
			"argument.literal.incorrect", []string{","}, chestOffset + 6},
		{"entry with no colon behind the name", "minecraft:chest{a 1}",
			"argument.literal.incorrect", []string{":"}, chestOffset + 3},
		{"name that never came", "minecraft:chest{:1}",
			"argument.literal.incorrect", []string{"\""}, chestOffset + 1},
		{"empty quoted name", "minecraft:chest{\"\":1}",
			"argument.literal.incorrect", []string{"."}, chestOffset + 5},
		{"leading zero", "minecraft:chest{a:01}",
			"argument.literal.incorrect", []string{"."}, chestOffset + 5},
		{"float with a letter behind it", "minecraft:chest{a:1.5q}",
			"argument.literal.incorrect", []string{"e|E"}, chestOffset + 6},
		{"array element that is not a number", "minecraft:chest{a:[I;1,b]}",
			"argument.literal.incorrect", []string{"+"}, chestOffset + 8},
		{"escape that is not one", "minecraft:chest{a:'\\q'}",
			"argument.literal.incorrect", []string{"b"}, chestOffset + 5},
		{"byte too wide for its suffix", "minecraft:chest{a:300b}",
			"snbt.parser.number_parse_failure", []string{"Value out of range. Value:\"300\" Radix:10"}, chestOffset + 7},
		{"operation that is not one", "minecraft:chest{a:foo(1)}",
			"snbt.parser.no_such_operation", []string{"foo/1"}, chestOffset + 9},
		{"data behind the compound", "minecraft:chest{}x",
			"command.expected.separator", nil, chestOffset + 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"
			_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			input := "setblock 2 64 3 " + tc.block
			c.ChatCommand(input)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(input, tc.cursor))
			c.Disconnect()
		})
	}

	// A compound that stops short of a value names the unquoted string the
	// grammar wanted where it stood.
	input := "setblock 2 64 3 chest{a:}"
	c.ChatCommand(input)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("snbt.parser.expected_unquoted_string"),
		servertest.CommandErrorContextForTest(input, prefix+len("chest")+len("{a:")))
	c.Disconnect()
}

// TestSetblockBambooStaysOnGrass pins the survival check a command's block
// goes through: a bamboo put by a command on a block bamboo may grow on is
// left where it was put.
func TestSetblockBambooStaysOnGrass(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	srv.SetLoadedBlock(2, 63, 3, registry.BlockStates["minecraft:grass_block"])
	bamboo, ok := registry.FindState("minecraft:bamboo", nil)
	if !ok {
		t.Fatal("the registry has no bamboo")
	}
	c.ChatCommand("setblock 2 64 3 bamboo")
	c.Drain()

	if got := srv.BlockAt(2, 64, 3); got != bamboo {
		t.Errorf("block at (2, 64, 3) = %d, want %d (bamboo, kept on the grass)", got, bamboo)
	}
	c.Disconnect()
}

// TestSetblockTagReadsVanillaShapedFields checks the fields a vanilla
// world writes in a shape this server's own writer does not: a container's
// stack with an int Slot and a byte count, the furnace's times as shorts, and
// the loot table and custom name a structure chest carries. The command lands
// the tag on the modelled entity.
func TestSetblockTagReadsVanillaShapedFields(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// A stack whose slot is an int and whose count a byte: the stack codec
	// narrows both, so the chest holds the diamond the tag named.
	c.ChatCommand(`setblock 2 64 3 minecraft:chest{Items:[{Slot:2,id:"minecraft:diamond",count:5b}]}`)
	c.Drain()
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	stacks := srv.InventoryAt(2, 64, 3)
	if len(stacks) != 27 {
		t.Fatalf("the chest holds %d slots, want 27", len(stacks))
	}
	if got := stacks[2]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("chest slot 2 = %+v, want five diamonds", got)
	}

	// A furnace whose times are written short, with an input to hold the smelt
	// open: the save writes them as ints, the width vanilla 26.3 writes.
	c.ChatCommand(`setblock 3 64 3 minecraft:furnace{Items:[{Slot:0b,id:"minecraft:raw_iron",count:1b}],cooking_time_spent:15s,cooking_total_time:200s,lit_time_remaining:80s,lit_total_time:200s}`)
	c.Drain()
	tag, ok := srv.BlockEntityTagAt(3, 64, 3)
	if !ok {
		t.Fatal("the furnace has no block entity tag")
	}
	if spent, ok := nbt.Get[nbt.Int](tag, "cooking_time_spent"); !ok || spent < 15 {
		t.Errorf("saved cooking_time_spent = %v, want an int of at least 15", tag["cooking_time_spent"])
	}
	if _, ok := nbt.Get[nbt.Int](tag, "lit_time_remaining"); !ok {
		t.Errorf("saved lit_time_remaining = %v, want an int", tag["lit_time_remaining"])
	}
	for _, name := range []string{"cooking_total_time", "lit_total_time"} {
		if got, ok := nbt.Get[nbt.Int](tag, name); !ok || got != 200 {
			t.Errorf("saved %s = %v, want the int 200", name, tag[name])
		}
	}

	// A chest's loot table and custom name survive the modelled decode and
	// save, rather than dropping with the kind's own items.
	c.ChatCommand(`setblock 4 64 3 minecraft:chest{LootTable:"minecraft:chests/simple_dungeon",CustomName:"{\"text\":\"Loot\"}"}`)
	c.Drain()
	tag, ok = srv.BlockEntityTagAt(4, 64, 3)
	if !ok {
		t.Fatal("the named chest has no block entity tag")
	}
	if got, _ := nbt.Get[nbt.String](tag, "LootTable"); got != "minecraft:chests/simple_dungeon" {
		t.Errorf("chest LootTable = %q, want the table", got)
	}
	if _, ok := tag["CustomName"]; !ok {
		t.Errorf("the chest dropped its CustomName: %v", tag)
	}
	c.Disconnect()
}
