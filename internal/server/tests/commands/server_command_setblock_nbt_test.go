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
