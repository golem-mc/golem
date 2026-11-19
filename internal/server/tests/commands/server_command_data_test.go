package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The /data command over storages, entities and block entities, checked
// against what a vanilla server answers the same commands with.

// dataWorld joins one operator, the level /data runs at.
func dataWorld(t *testing.T, name string) *servertest.Client {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	return c
}

// TestCommandDataStorageMergeGetAndModify checks the /data forms the
// storages answer with, wording included: the query a bare get or a path
// get prints, the scaled form's wording, the modify forms that write, and
// the refusals the reads of what is not there answer with.
func TestCommandDataStorageMergeGetAndModify(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:a {x:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	// The issue's own done: a path get replies with the storage and the
	// value the path names.
	c.ChatCommand("data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1")

	// The bare get shows the whole compound, pretty-printed with the
	// spacing vanilla's NBT text component puts between entries.
	c.ChatCommand("data get storage test:a")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "{x: 1}")

	// The scaled get names the path, the storage, the scale at two
	// decimals and the product, floored, as a number component.
	five := int64(5)
	c.ChatCommand("data get storage test:a x 5")
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "x"}, protocol.Text{Text: "test:a"},
		protocol.Text{Text: "5.00"}, protocol.Text{Number: &five})

	// Modify writes through its value and from forms.
	c.ChatCommand("data modify storage test:a y set value 5")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("data get storage test:a y")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "5")
	c.ChatCommand("data modify storage test:a y set from storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("data get storage test:a y")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1")

	// Remove takes one path out; a second remove of it changes nothing,
	// which vanilla answers with the merge-unchanged refusal.
	c.ChatCommand("data remove storage test:a y")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("data remove storage test:a y")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.merge.failed"))
	c.ChatCommand("data get storage test:a y")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "y"))

	// Only numeric tags take a scale.
	c.ChatCommand("data merge storage test:a {z:\"hi\"}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("data get storage test:a z 2")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.get.invalid", "z"))

	// A storage nobody has written reads as empty; a path into it reads
	// nothing, as vanilla's own empty storage does.
	c.ChatCommand("data get storage gone:g")
	c.ExpectCommandFeedback("commands.data.storage.query", "gone:g", "{}")
	c.ChatCommand("data get storage gone:g p")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "p"))

	// Modify creates the storage it writes, and a list path it creates
	// grows by the inserts; a path naming a compound refuses an index
	// into it the way the list insert does not.
	c.ChatCommand("data modify storage test:c list append value 3")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:c")
	c.ChatCommand("data get storage test:c list")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:c", "[3]")
	c.ChatCommand("data modify storage test:c list insert 0 value 2")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:c")
	c.ChatCommand("data get storage test:c list")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:c", "[2, 3]")
	c.ChatCommand("data modify storage test:c list insert 2 value 4")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:c")
	// A path naming more than one tag answers the single-value refusal
	// the get takes.
	c.ChatCommand("data get storage test:c list[]")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.get.multiple"))

	// A path may carry a quoted key with a space in it, spanning the
	// words the tokenizer split it into.
	c.ChatCommand("data modify storage test:c \"two words\" set value 5")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:c")
	c.ChatCommand("data get storage test:c \"two words\"")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:c", "5")

	c.Disconnect()
}

// TestCommandDataMergeEmptyTypedArray checks that a typed array with no
// elements is a value the compound argument takes: the merge answers the
// modified feedback and a get prints the empty array back.
// vanilla: SnbtGrammar list_literal
func TestCommandDataMergeEmptyTypedArray(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	for _, array := range []string{"[B;]", "[I;]", "[L;]"} {
		c.ChatCommand("data merge storage t:r {m:" + array + "}")
		c.ExpectCommandFeedback("commands.data.storage.modified", "t:r")
		c.ChatCommand("data get storage t:r m")
		c.ExpectCommandFeedback("commands.data.storage.query", "t:r", array)
	}

	c.Disconnect()
}

// TestCommandDataStoragePersistsOverARestart checks the issue's own done:
// after a shutdown the storage sits in the file vanilla reads — the
// namespace's command_storage.dat with a contents compound keyed by the
// storage id's path — and a server restarted on the same world answers a
// get of it.
func TestCommandDataStoragePersistsOverARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("data merge storage test:a {x:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	// A merge reaches into the compound an entry already holds, and an
	// array element edited in place still marks the file dirty, so a
	// restart reads both back.
	c.ChatCommand("data merge storage test:m {o:{a:{x:1}}}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:m")
	c.ChatCommand("data modify storage test:m o merge value {a:{y:2}}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:m")
	c.ChatCommand("data merge storage test:arr {arr:[I;1,2]}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.ChatCommand("data modify storage test:arr arr[0] set value 9")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.Disconnect()
	srv.Shutdown()

	root := servertest.ReadPlayerFile(t, commandStorageFile(cfg.LevelName, "test"))
	data, ok := root["data"].(nbt.Compound)
	if !ok {
		t.Fatalf("command storage file holds no data compound: %v", root)
	}
	contents, ok := data["contents"].(nbt.Compound)
	if !ok {
		t.Fatalf("command storage data holds no contents compound: %v", data)
	}
	if got, ok := nbt.Get[nbt.Compound](contents, "a"); !ok || got["x"] != nbt.Int(1) {
		t.Errorf("contents[a] = %#v, want {x: 1}", contents["a"])
	}
	if stored, ok := nbt.Get[nbt.Compound](contents, "arr"); ok {
		if arr, ok := nbt.Get[nbt.IntArray](stored, "arr"); !ok || len(arr) != 2 || arr[0] != 9 || arr[1] != 2 {
			t.Errorf("contents[arr][arr] = %#v, want the edited int array", stored["arr"])
		}
	} else {
		t.Errorf("contents[arr] = %#v, want the storage compound", contents["arr"])
	}
	if _, ok := root["DataVersion"]; !ok {
		t.Error("command storage file has no DataVersion, want the saved-data form")
	}

	restarted := *cfg
	srv2, reconnect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Testificate"))
	c = reconnect()
	c.Join("Testificate")
	c.ChatCommand("data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1")
	c.ChatCommand("data get storage test:m o.a")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:m", "{x: 1, y: 2}")
	c.ChatCommand("data get storage test:arr arr[0]")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:arr", "9")
	c.Disconnect()
	_ = srv2
}

// TestCommandDataGetEntityNamesItsHealth checks the entity get: the scoped
// query of a summoned cow's Health names the cow and its health the way
// vanilla writes the tag.
func TestCommandDataGetEntityNamesItsHealth(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))

	c.ChatCommand("data get entity @e[type=minecraft:cow,limit=1] Health")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid),
		servertest.NBTArg("10.0f"))

	// The scaled reader floors the product and names the path and the body;
	// the product rides the wire as a number.
	twenty := int64(20)
	c.ChatCommand("data get entity @e[type=minecraft:cow,limit=1] Health 2")
	c.ExpectCommandFeedbackText("commands.data.entity.get",
		protocol.Text{Text: "Health"},
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid),
		protocol.Text{Text: "2.00"},
		protocol.Text{Number: &twenty})

	// A path the body does not keep answers with the not-found refusal.
	c.ChatCommand("data get entity @e[type=minecraft:cow,limit=1] nope")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "nope"))

	c.Disconnect()
}

// TestCommandDataBlockEntityReadsItsTag checks the block get and the
// refusals of a block with no data of its own: the chest's block entity
// answers the get of the id the whole-registry tag keeps, and a plain block
// answers with the not-a-block-entity refusal.
func TestCommandDataBlockEntityReadsItsTag(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"))

	c.ChatCommand("setblock 1 63 1 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")

	c.ChatCommand("data get block 1 63 1 id")
	c.ExpectCommandFeedback("commands.data.block.query", "1", "63", "1", "\"minecraft:chest\"")

	c.ChatCommand("data get block 2 63 2")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.block.invalid"))

	c.Disconnect()
}

// TestExecuteIfDataStorageConditional checks the conditional the existing
// /execute tree offers once storages hold something: a path with a value
// under it passes, counting the matches the way the other counting
// conditionals do, and one without fails; the unless form turns them about.
func TestExecuteIfDataStorageConditional(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:a {x:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	one := int64(1)
	c.ChatCommand("execute if data storage test:a x")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &one})
	c.ChatCommand("execute if data storage test:a nope")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
	c.ChatCommand("execute unless data storage test:a x")
	c.ExpectCommandFailure(conditionalFailCountForTest(one))
	c.ChatCommand("execute unless data storage test:a nope")
	c.ExpectCommandFeedback("commands.execute.conditional.pass")

	c.Disconnect()
}

// TestCommandDataStorageBareIDNamesTheNamespace checks the messages a bare
// storage id answers with: the id reads as an identifier, its namespace
// spelled out.
func TestCommandDataStorageBareIDNamesTheNamespace(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage plain {x:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:plain")
	c.ChatCommand("data get storage plain x")
	c.ExpectCommandFeedback("commands.data.storage.query", "minecraft:plain", "1")

	c.Disconnect()
}

// TestCommandDataMergeTakesATrailingComma checks that a list or a typed
// array in a merged compound may end in one comma. vanilla: SnbtGrammar
// list_entries, int_array_entries
func TestCommandDataMergeTakesATrailingComma(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage t:r {m:[1,]}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:r")
	c.ChatCommand("data get storage t:r m")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:r", "[1]")

	c.ChatCommand("data merge storage t:r {m:[I;1,]}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:r")
	c.ChatCommand("data get storage t:r m")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:r", "[I; 1]")

	c.Disconnect()
}

// TestCommandDataMergeReachesIntoNestedCompounds checks that a merge onto
// a name both sides hold reaches into the compound: the target keeps the
// entries it had under it.
func TestCommandDataMergeReachesIntoNestedCompounds(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:deep {o:{a:{x:1}}}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:deep")
	c.ChatCommand("data merge storage test:deep {o:{a:{y:2}}}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:deep")
	c.ChatCommand("data get storage test:deep o.a")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:deep", "{x: 1, y: 2}")

	// A modify merge reaches in the same way.
	c.ChatCommand("data modify storage test:deep o merge value {a:{z:3}}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:deep")
	c.ChatCommand("data get storage test:deep o.a")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:deep", "{x: 1, y: 2, z: 3}")

	c.Disconnect()
}

// TestCommandDataArrayOperations checks the typed arrays as the collections
// the path reads them as: they grow and shrink, and an element a value
// cannot narrow changes nothing.
func TestCommandDataArrayOperations(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:arr {arr:[I;1,2]}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.ChatCommand("data modify storage test:arr arr append value 3")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.ChatCommand("data get storage test:arr arr")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:arr", "[I; 1, 2, 3]")
	c.ChatCommand("data modify storage test:arr arr prepend value 0")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.ChatCommand("data get storage test:arr arr")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:arr", "[I; 0, 1, 2, 3]")
	c.ChatCommand("data remove storage test:arr arr[0]")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:arr")
	c.ChatCommand("data get storage test:arr arr")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:arr", "[I; 1, 2, 3]")
	c.ChatCommand("data modify storage test:arr arr[0] set value \"s\"")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.merge.failed"))
	c.ChatCommand("data get storage test:arr arr[0]")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:arr", "1")

	c.Disconnect()
}

// TestCommandDataEntityWriteLoadsTheTag checks a merge onto a non-player
// body: the compound loads onto the live entity, so a get reads it back.
// A merge that changes nothing refuses first; a player's write is the one
// the command refuses as invalid.
func TestCommandDataEntityWriteLoadsTheTag(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))

	c.ChatCommand("data merge entity @e[type=minecraft:cow,limit=1] {NoAI:1b}")
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	c.ChatCommand("data get entity @e[type=minecraft:cow,limit=1] NoAI")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid),
		servertest.NBTArg("1b"))

	c.ChatCommand("data merge entity @e[type=minecraft:cow,limit=1] {}")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.merge.failed"))
	c.ChatCommand("data merge entity @e[type=minecraft:cow,limit=1] {NoAI:1b}")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.merge.failed"))
	c.ChatCommand("data merge entity @p {NoAI:1b}")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.entity.invalid"))

	c.Disconnect()
}

// TestCommandDataModifyStringWithoutSourcePath checks the string form with
// its source path left out: the source's whole compound is no value the
// string can read, which the command refuses.
func TestCommandDataModifyStringWithoutSourcePath(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:s {x:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:s")
	c.ChatCommand("data modify storage test:out y set string storage test:s")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.modify.expected_value", "{x:1}"))

	// A from without a source path takes the whole compound.
	c.ChatCommand("data modify storage test:out y set from storage test:s")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:out")
	c.ChatCommand("data get storage test:out y")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:out", "{x: 1}")

	c.Disconnect()
}

// TestCommandDataScaledGetSaturatesAndRounds checks the scaled read's two
// arithmetic quirks: the product floors into an int that saturates, and
// the scale prints at two decimals with halves rounded up.
func TestCommandDataScaledGetSaturatesAndRounds(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:big {n:1000000000000L}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:big")
	saturated := int64(2147483647)
	c.ChatCommand("data get storage test:big n 1")
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "n"}, protocol.Text{Text: "test:big"},
		protocol.Text{Text: "1.00"}, protocol.Text{Number: &saturated})

	c.ChatCommand("data merge storage test:round {v:8}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:round")
	eight := int64(1)
	c.ChatCommand("data get storage test:round v 0.125")
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "v"}, protocol.Text{Text: "test:round"},
		protocol.Text{Text: "0.13"}, protocol.Text{Number: &eight})

	// The scale's shortest decimal form is what rounds, not its exact binary
	// value, as Java's "%.2f" rounds the double's own text.
	c.ChatCommand("data merge storage test:half {n:1}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:half")
	one := int64(1)
	c.ChatCommand("data get storage test:half n 1.005")
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "n"}, protocol.Text{Text: "test:half"},
		protocol.Text{Text: "1.01"}, protocol.Text{Number: &one})
	c.ChatCommand("data get storage test:half n 2.675")
	two := int64(2)
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "n"}, protocol.Text{Text: "test:half"},
		protocol.Text{Text: "2.68"}, protocol.Text{Number: &two})
	c.ChatCommand("data get storage test:half n 0.015")
	zero := int64(0)
	c.ExpectCommandFeedbackText("commands.data.storage.get",
		protocol.Text{Text: "n"}, protocol.Text{Text: "test:half"},
		protocol.Text{Text: "0.02"}, protocol.Text{Number: &zero})

	c.Disconnect()
}

// TestCommandDataEntityWriteTellsTheClient checks a merge onto a live body
// reaches the clients shown it: the metadata the write turned goes out the
// way a vanilla setter's marked synced data does — a name, an item's stack,
// a primed TNT's fuse and the no-gravity flag.
func TestCommandDataEntityWriteTellsTheClient(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	c.ChatCommand(`data merge entity @e[type=minecraft:cow,limit=1] {CustomName:"Bob",CustomNameVisible:1b}`)
	// awaitMetadataFor steps over the command's feedback line to reach the
	// metadata the merge broadcast.
	named := c.AwaitMetadataFor(cow.EntityID)
	if m := servertest.MetaOf(named, protocol.MetadataIndexCustomName); m == nil || !reflect.DeepEqual(m.TextVal, nbt.String("Bob")) {
		t.Errorf("the cow's custom name metadata = %+v, want the string Bob", m)
	}
	if m := servertest.MetaOf(named, protocol.MetadataIndexCustomNameVisible); m == nil || !m.FlagVal {
		t.Errorf("the cow's name-visible metadata = %+v, want shown always", m)
	}

	c.ChatCommand(`summon minecraft:item 4.5 64 4.5 {Item:{id:"minecraft:stone",count:1}}`)
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	c.AwaitMetadataFor(item.EntityID) // the spawn's own stack
	c.ChatCommand(`data merge entity @e[type=minecraft:item,limit=1] {Item:{id:"minecraft:diamond",count:3},NoGravity:1b}`)
	restacked := c.AwaitMetadataFor(item.EntityID)
	if m := servertest.MetaOf(restacked, protocol.MetadataIndexItemSlot); m == nil {
		t.Error("the item's metadata carries no stack after the merge")
	} else if name, _ := registry.ItemName(m.SlotVal.ItemID); name != "minecraft:diamond" || m.SlotVal.Count != 3 {
		t.Errorf("the item's merged stack = %s x%d, want minecraft:diamond x3", name, m.SlotVal.Count)
	}
	if m := servertest.MetaOf(restacked, protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
		t.Errorf("the item's no-gravity metadata = %+v, want the flag set", m)
	}

	c.ChatCommand("summon minecraft:tnt 6.5 64 6.5 {fuse:1000s}")
	tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
	c.AwaitMetadataFor(tnt.EntityID) // the spawn's own fuse
	c.ChatCommand("data merge entity @e[type=minecraft:tnt,limit=1] {fuse:5s}")
	c.AwaitMetadataFuse(tnt.EntityID, 5)

	c.Disconnect()
}

// TestCommandDataMergeArmorStandCarriesTheWrite merges onto a summoned
// armor stand: the flags, the no-gravity flag and a pose read back off the
// metadata the write told, the slot mask off its own get, and a get reads
// them back the way the merge left them.
func TestCommandDataMergeArmorStandCarriesTheWrite(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 2.5 {ShowArms:1b}`)
	spawn := c.ExpectArmorStandSpawn()
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))
	// expectArmorStandSpawn already read the spawn's own metadata packet.
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] ` +
		`{Invisible:1b,Small:1b,NoGravity:1b,Pose:{Head:[45f,0f,0f]},DisabledSlots:2048}`)
	// The metadata the merge broadcast rides ahead of the command's
	// feedback line, so the read reaches it first.
	told := c.AwaitMetadataFor(spawn.EntityID)
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))
	if m := servertest.MetaOf(told, protocol.MetadataIndexEntityFlags); m == nil || m.ByteVal&protocol.EntityFlagInvisible == 0 {
		t.Errorf("the stand's flags metadata = %+v, want the invisible bit", m)
	}
	if m := servertest.MetaOf(told, protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
		t.Errorf("the stand's no-gravity metadata = %+v, want the flag set", m)
	}
	if m := servertest.MetaOf(told, protocol.MetadataIndexArmorStandFlags); m == nil ||
		m.ByteVal&protocol.ArmorStandFlagSmall == 0 || m.ByteVal&protocol.ArmorStandFlagArms == 0 {
		t.Errorf("the stand's own flags metadata = %+v, want small and arms", m)
	}
	if m := servertest.MetaOf(told, protocol.MetadataIndexArmorStandHead); m == nil || m.RotVal != [3]float32{45, 0, 0} {
		t.Errorf("the Head rotation metadata = %+v, want (45, 0, 0)", m)
	}

	c.ChatCommand("data get entity @e[type=minecraft:armor_stand,limit=1] Invisible")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity),
		servertest.NBTArg("1b"))
	// The slot mask is not on the wire: the stand keeps it and reads it back
	// off its own state.
	c.ChatCommand("data get entity @e[type=minecraft:armor_stand,limit=1] DisabledSlots")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity),
		servertest.NBTArg("2048"))

	c.Disconnect()
}

func TestCommandDataRemoveArmorStandCustomName(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`summon minecraft:armor_stand ~ ~ ~ {CustomName:"Hi"}`)
	spawn := c.ExpectArmorStandSpawn()
	if !reflect.DeepEqual(spawn.Name, nbt.String("Hi")) {
		t.Fatalf("summoned stand's name = %v, want Hi", spawn.Name)
	}
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")

	c.ChatCommand(`data remove entity @e[type=minecraft:armor_stand,limit=1] CustomName`)
	told := c.AwaitMetadataFor(spawn.EntityID)
	if name := servertest.MetaOf(told, protocol.MetadataIndexCustomName); name == nil || name.TextVal != nil {
		t.Errorf("removed name's metadata = %+v, want an absent optional text", name)
	}
	c.ExpectFeedbackLine("commands.data.entity.modified")
	c.ChatCommand(`data get entity @e[type=minecraft:armor_stand,limit=1] CustomName`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "CustomName"))
}

// TestCommandDataMergeArmorStandMovesAndResets loads a place, a speed and a
// tag onto a summoned stand: the whole entity is read back, so a get reads
// the new place and speed and /tag finds the tag. A second merge that puts
// the no-gravity flag and the head pose back to the defaults a fresh client
// holds names those fields too, so the metadata carries them again.
func TestCommandDataMergeArmorStandMovesAndResets(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 2.5`)
	spawn := c.ExpectArmorStandSpawn()
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))

	// NoGravity holds the stand, so the speed read back is the one the merge
	// wrote rather than one the walk has dragged.
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] ` +
		`{NoGravity:1b,Motion:[0.25d,0d,0d],Pos:[8.5d,64d,8.5d],Pose:{Head:[45f,0f,0f]},Tags:["probe"]}`)
	c.AwaitMetadataFor(spawn.EntityID)
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))

	c.ChatCommand("data get entity @e[type=minecraft:armor_stand,limit=1] Pos[0]")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity),
		servertest.NBTArg("8.5d"))
	c.ChatCommand("data get entity @e[type=minecraft:armor_stand,limit=1] Motion[0]")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity),
		servertest.NBTArg("0.25d"))
	c.ChatCommand("tag @e[type=minecraft:armor_stand,limit=1] list")
	c.ExpectCommandFeedbackText("commands.tag.list.single.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity),
		servertest.TagCountText(1), servertest.GreenTagText("probe"))

	// The flag and the pose back to the defaults: the write must name them,
	// so the metadata a fresh client needs carries them again.
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] ` +
		`{NoGravity:0b,Pose:{Head:[0f,0f,0f]}}`)
	told := c.AwaitMetadataFor(spawn.EntityID)
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))
	if m := servertest.MetaOf(told, protocol.MetadataIndexNoGravity); m == nil || m.FlagVal {
		t.Errorf("the stand's no-gravity metadata = %+v, want the flag cleared", m)
	}
	if m := servertest.MetaOf(told, protocol.MetadataIndexArmorStandHead); m == nil || m.RotVal != [3]float32{0, 0, 0} {
		t.Errorf("the Head rotation metadata = %+v, want the default", m)
	}

	c.Disconnect()
}

// TestCommandDataMergeArmorStandTellsTheEquipment merges a piece onto a
// summoned stand: the slot the merge changed follows the metadata on a Set
// Equipment packet, the way LivingEntity.detectEquipmentUpdates sends it, so
// the piece shows on a watcher that already knew the stand.
func TestCommandDataMergeArmorStandTellsTheEquipment(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 2.5`)
	spawn := c.ExpectArmorStandSpawn()
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))

	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] ` +
		`{equipment:{head:{id:"minecraft:stone",count:1}}}`)
	c.AwaitMetadataFor(spawn.EntityID)
	equipment := c.ExpectEquipment(spawn.EntityID)
	if got := servertest.EquipEntry(equipment, 5); got.ItemID != servertest.RegistryItemID("minecraft:stone") {
		t.Errorf("the head slot = %+v, want the merged stone", got)
	}
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))

	c.Disconnect()
}

// TestCommandDataMergeThrownCarriesTheWrite throws a snowball and merges
// onto it: the no-gravity flag rides the metadata the write told, and a
// get reads it back.
func TestCommandDataMergeThrownCarriesTheWrite(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Thrower", servertest.OpsListing(t, "Thrower"))
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.RegistryItemID("minecraft:snowball"), Count: 4})

	// A throw pitched straight up, so the snowball is still in the air
	// when the merge lands on it.
	c.MoveTo(0.5, 64, 0.5, -90, -90)
	servertest.UseHeld(c, -90, -90)
	ball := c.ExpectThrownSpawn(srv, protocol.EntityTypeSnowball)
	c.AwaitMetadataFor(ball.EntityID) // the spawn's own stack

	c.ChatCommand(`data merge entity @e[type=minecraft:snowball,limit=1] {NoGravity:1b}`)
	// The metadata the merge broadcast rides ahead of the command's
	// feedback line, so the read reaches it first. A flight's own metadata
	// can still be on the wire ahead of it; the merge's is the one naming
	// the flag.
	told := c.AwaitMetadataFor(ball.EntityID)
	for servertest.MetaOf(told, protocol.MetadataIndexNoGravity) == nil {
		told = c.AwaitMetadataFor(ball.EntityID)
	}
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:snowball", "entity.minecraft.snowball", ball.Uuid))
	if m := servertest.MetaOf(told, protocol.MetadataIndexNoGravity); m == nil || !m.FlagVal {
		t.Errorf("the snowball's no-gravity metadata = %+v, want the flag set", m)
	}

	c.ChatCommand("data get entity @e[type=minecraft:snowball,limit=1] NoGravity")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:snowball", "entity.minecraft.snowball", ball.Uuid),
		servertest.NBTArg("1b"))

	c.Disconnect()
}

// TestCommandDataRemoveItemStackDiscardsTheBody checks a write that leaves an
// item entity with no stack discards it, as vanilla's ItemEntity read does:
// the stack gone and the body taken back out of the world.
func TestCommandDataRemoveItemStackDiscardsTheBody(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`summon minecraft:item 2.5 64 2.5 {Item:{id:"minecraft:stone",count:1}}`)
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	c.AwaitMetadataFor(item.EntityID)

	c.ChatCommand("data remove entity @e[type=minecraft:item,limit=1] Item")
	gone := protocol.NewReader(c.AwaitPlay(protocol.IDPlayRemoveEntities))
	if n := gone.ReadVarInt(); n != 1 || gone.ReadVarInt() != item.EntityID {
		t.Errorf("the removal names %d entities of %d, want the one item", n, item.EntityID)
	}

	c.Disconnect()
}

// commandStorageFile is the saved-data file a namespace's storages live in.
func commandStorageFile(level, namespace string) string {
	return level + "/data/" + namespace + "/command_storage.dat"
}

// TestCommandDataModifyCompute checks the compute source: an int provider
// stores an int, a float provider a float, and the entity context hands
// the provider its target.
func TestCommandDataModifyCompute(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`data modify storage t:s v set compute default integer {type:"minecraft:add",inputs:[2,3]}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("data get storage t:s v")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "5")

	c.ChatCommand("data modify storage t:s f set compute default float 1.5")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("data get storage t:s f")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "1.5f")

	c.ChatCommand("scoreboard objectives add x dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set @s x 7")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.ChatCommand(`data modify storage t:s l append compute entity @s integer {type:"minecraft:score",target:"target_entity",score:"x"}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("data get storage t:s l")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "[7]")

	// A failed int provider and a non-finite float both store 0.
	c.ChatCommand(`data modify storage t:s v set compute default integer {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("data get storage t:s v")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "0")

	c.ChatCommand(`data modify storage t:s f set compute default float {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:s")
	c.ChatCommand("data get storage t:s f")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "0.0f")

	c.Disconnect()
}

// TestCommandDataModifyComputeTree checks the source forms every modify
// operation offers, and the compute subtree, against 26.3's commands.json.
func TestCommandDataModifyComputeTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	c.SkipJoin()

	child := func(i int32, name string) int32 {
		t.Helper()
		for _, k := range nodes[i].Children {
			if nodes[k].Name == name {
				return k
			}
		}
		t.Fatalf("node %q has no child %q", nodes[i].Name, name)
		return -1
	}
	names := func(i int32) []string {
		var out []string
		for _, k := range nodes[i].Children {
			out = append(out, nodes[k].Name)
		}
		slices.Sort(out)
		return out
	}
	parsers := map[int32]string{6: "entity", 8: "block_pos", 55: "context_float_provider", 56: "context_int_provider"}
	var describe func(i int32, depth int) string
	describe = func(i int32, depth int) string {
		n := nodes[i]
		line := strings.Repeat(" ", depth) + n.Name
		if n.Typ == 2 {
			line += " <" + parsers[n.Parser] + ">"
		}
		if n.Executable {
			line += " exec"
		}
		line += "\n"
		kids := slices.Clone(n.Children)
		slices.SortFunc(kids, func(a, b int32) int { return strings.Compare(nodes[a].Name, nodes[b].Name) })
		for _, k := range kids {
			line += describe(k, depth+1)
		}
		return line
	}
	const want = `compute
 block
  computePos <block_pos>
   float
    provider <context_float_provider> exec
   integer
    provider <context_int_provider> exec
 default
  float
   provider <context_float_provider> exec
  integer
   provider <context_int_provider> exec
 entity
  computeTarget <entity>
   float
    provider <context_float_provider> exec
   integer
    provider <context_int_provider> exec
`
	modify := child(child(0, "data"), "modify")
	for _, kind := range []string{"entity", "block", "storage"} {
		k := child(modify, kind)
		path := child(nodes[k].Children[0], "targetPath")
		for _, op := range []string{"insert", "prepend", "append", "set", "merge"} {
			o := child(path, op)
			if op == "insert" {
				o = child(o, "index")
			}
			if got := names(o); !slices.Equal(got, []string{"compute", "from", "string", "value"}) {
				t.Errorf("%s %s sources = %v", kind, op, got)
			}
			if got := describe(child(o, "compute"), 0); got != want {
				t.Errorf("%s %s compute subtree:\n%s\nwant:\n%s", kind, op, got, want)
			}
		}
	}
	c.Disconnect()
}

// TestCommandDataMergeItemFrameCarriesTheWrite merges flags, an item and a
// rotation onto a summoned item frame and reads them off the metadata the
// write told, then clears Invisible.
func TestCommandDataMergeItemFrameCarriesTheWrite(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))

	c.ChatCommand("summon minecraft:item_frame 5.5 64 5.5 {Facing:1b}")
	frame := c.AwaitSpawn(protocol.EntityTypeItemFrame)
	c.AwaitMetadataFor(frame.EntityID)
	c.ChatCommand(`data merge entity @e[type=minecraft:item_frame,limit=1] {Invisible:1b,Fixed:1b,ItemRotation:3b,Item:{id:"minecraft:diamond",count:1}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	meta := c.AwaitMetadataFor(frame.EntityID)
	if m := servertest.MetaOf(meta, protocol.MetadataIndexEntityFlags); m == nil || m.ByteVal&protocol.EntityFlagInvisible == 0 {
		t.Errorf("the frame's flags metadata = %+v, want invisible", m)
	}
	if m := servertest.MetaOf(meta, protocol.MetadataIndexFrameItem); m == nil {
		t.Error("the frame's metadata carries no item")
	} else if name, _ := registry.ItemName(m.SlotVal.ItemID); name != "minecraft:diamond" {
		t.Errorf("the frame's item = %s, want minecraft:diamond", name)
	}
	if m := servertest.MetaOf(meta, protocol.MetadataIndexFrameRotation); m == nil || m.VarintVal != 3 {
		t.Errorf("the frame's rotation metadata = %+v, want 3", m)
	}

	c.ChatCommand("data merge entity @e[type=minecraft:item_frame,limit=1] {Invisible:0b}")
	shown := c.AwaitMetadataFor(frame.EntityID)
	if m := servertest.MetaOf(shown, protocol.MetadataIndexEntityFlags); m == nil || m.ByteVal&protocol.EntityFlagInvisible != 0 {
		t.Errorf("the frame's flags metadata = %+v, want visible", m)
	}
	c.Disconnect()
}

// TestCommandDataMergeFacingTurnsAnItemFrame turns a floor frame to face
// south: the write tells the new direction, and with nothing behind it the
// frame drops at its next survives poll.
// vanilla: ItemFrame.readAdditionalSaveData, ItemFrame.setDirection,
// BlockAttachedEntity.tick
func TestCommandDataMergeFacingTurnsAnItemFrame(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	if srv.BlockAt(5, 63, 5) == 0 || srv.BlockAt(5, 64, 4) != 0 {
		t.Fatalf("the floor holds %d and the cell north of the frame %d, want ground under air",
			srv.BlockAt(5, 63, 5), srv.BlockAt(5, 64, 4))
	}

	c.ChatCommand("summon minecraft:item_frame 5.5 64 5.5 {Facing:1b}")
	frame := c.AwaitSpawn(protocol.EntityTypeItemFrame)
	c.AwaitMetadataFor(frame.EntityID)
	c.ChatCommand("data merge entity @e[type=minecraft:item_frame,limit=1] {Facing:3b}")
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	meta := c.AwaitMetadataFor(frame.EntityID)
	if m := servertest.MetaOf(meta, protocol.MetadataIndexHangingDirection); m == nil || m.VarintVal != protocol.FaceSouth {
		t.Fatalf("the frame's direction metadata = %+v, want south", m)
	}

	// The survives poll runs every 101 ticks of the frame's life.
	servertest.HoldTicks(t, srv)
	if !servertest.ASpawnOfWithinTicks(c, srv, 110, protocol.EntityTypeItem) {
		t.Error("the turned frame dropped nothing")
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames, want the unsupported one broken", n)
	}
	c.Disconnect()
}
