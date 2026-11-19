package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
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
