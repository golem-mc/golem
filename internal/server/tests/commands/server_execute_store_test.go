package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// The /execute store kinds that write NBT, each handed the return value or
// the success of the command the chain ends in.
// vanilla: ExecuteCommand.storeData

// TestExecuteStoreWritesTheResultIntoStorage checks the storage store: the
// result of the run's command scaled and cast to the type word, a success
// written as 1 or 0, a conditional's count, and a nested execute's.
func TestExecuteStoreWritesTheResultIntoStorage(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:a {x:7}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	c.ChatCommand("execute store result storage test:a y int 3 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a y")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "21")

	// The scaled value is cast the way Java casts a double: 3.5 stays a
	// double, and 700 wraps into a byte through the int.
	c.ChatCommand("execute store result storage test:a d double 0.5 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a d")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "3.5d")
	c.ChatCommand("execute store result storage test:a b byte 100 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a b")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "-68b")

	// A refused command stores 0 as its success, a passing one 1.
	c.ChatCommand("execute store success storage test:a s byte 1 run data get storage test:a nope")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "nope"))
	c.ChatCommand("data get storage test:a s")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "0b")
	c.ChatCommand("execute store success storage test:a s byte 1 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a s")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1b")

	// A chain ending on a counting conditional stores the count, directly
	// and through a nested execute.
	for range 2 {
		c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}
	two := int64(2)
	c.ChatCommand("execute store result storage test:a n int 1 if entity @e[type=minecraft:cow]")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &two})
	c.ChatCommand("data get storage test:a n")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "2")
	c.ChatCommand("execute store result storage test:a m int 1 run execute if entity @e[type=minecraft:cow]")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &two})
	c.ChatCommand("data get storage test:a m")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "2")

	// A failing conditional stores 0.
	c.ChatCommand("execute store result storage test:a n int 1 if entity @e[type=minecraft:pig]")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
	c.ChatCommand("data get storage test:a n")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "0")

	c.Disconnect()
}

// TestExecuteStoreWritesIntoAnEntity checks the entity store: the value
// loads onto the live body.
func TestExecuteStoreWritesIntoAnEntity(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	display := servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid)
	c.ExpectCommandFeedbackText("commands.summon.success", display)

	c.ChatCommand("execute store result entity @e[type=minecraft:cow,limit=1] Health float 0.5 run data get entity @e[type=minecraft:cow,limit=1] Health")
	c.ExpectCommandFeedbackText("commands.data.entity.query", display, servertest.NBTArg("10.0f"))
	c.ChatCommand("data get entity @e[type=minecraft:cow,limit=1] Health")
	c.ExpectCommandFeedbackText("commands.data.entity.query", display, servertest.NBTArg("5.0f"))

	c.Disconnect()
}

// TestExecuteStoreWritesIntoABlockEntity checks the block store: the value
// lands in the block entity's compound.
func TestExecuteStoreWritesIntoABlockEntity(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"))

	c.ChatCommand("setblock 1 63 1 minecraft:command_block")
	c.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")
	c.ChatCommand("data merge storage test:a {x:4}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	c.ChatCommand("execute store result block 1 63 1 SuccessCount int 1 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "4")
	c.ChatCommand("data get block 1 63 1 SuccessCount")
	c.ExpectCommandFeedback("commands.data.block.query", "1", "63", "1", "4")

	c.Disconnect()
}

// TestExecuteStoreRefusesABlockOutsideTheWorld checks that a block store
// refuses a position above the world's height, loaded or not.
func TestExecuteStoreRefusesABlockOutsideTheWorld(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("execute store result block 0 100000 0 foo byte 1 run say hi")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.pos.outofworld"))

	c.Disconnect()
}

// TestExecuteStoreRefusesABlockWithoutABlockEntity checks that a loaded
// block that keeps no block entity refuses the store.
func TestExecuteStoreRefusesABlockWithoutABlockEntity(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("execute store result block 0 63 0 foo byte 1 run say hi")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.data.block.invalid"))

	c.Disconnect()
}

// TestExecuteStoreOnAForkedChain checks that a store ahead of a fork is
// written by every branch.
func TestExecuteStoreOnAForkedChain(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	for range 2 {
		c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}
	c.ChatCommand("data merge storage test:a {x:7}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	c.ChatCommand("execute store result storage test:a n int 1 as @e[type=minecraft:cow] run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a n")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")

	c.Disconnect()
}

// TestExecuteStoreOfAnUnlessConditional checks the values an unless stores:
// 1 where it passes, 0 where it fails.
func TestExecuteStoreOfAnUnlessConditional(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))

	c.ChatCommand("execute store result storage test:a u int 1 unless entity @e[type=minecraft:pig]")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass")
	c.ChatCommand("data get storage test:a u")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1")

	c.ChatCommand("execute store result storage test:a u int 1 unless entity @e[type=minecraft:cow]")
	c.ExpectCommandFailure(conditionalFailCountForTest(1))
	c.ChatCommand("data get storage test:a u")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "0")

	c.Disconnect()
}

// TestExecuteStoreIntoABossbarValue checks that the value store moves the
// bar's fill and tells the players on it.
func TestExecuteStoreIntoABossbarValue(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	bar := expectBossBarAdd(t, bob, "The Test Bar", 0)
	alice.ChatCommand("data merge storage test:a {x:50}")
	alice.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	alice.ChatCommand("execute store result bossbar test:bar value run data get storage test:a x")
	alice.ExpectCommandFeedback("commands.data.storage.query", "test:a", "50")
	expectBossBarProgress(t, bob, bar, 0.5)

	alice.Disconnect()
	bob.Disconnect()
}

// TestExecuteStoreIntoAScore checks the score store: the result or the
// success written to every holder named, a selector's bodies by name, and
// the * that names every holder the board knows.
// vanilla: ExecuteCommand.storeValue
func TestExecuteStoreIntoAScore(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add points dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("data merge storage test:a {x:7}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	expectScore := func(holder string, want int64) {
		t.Helper()
		c.ChatCommand("scoreboard players get " + holder + " points")
		with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
		if len(with) != 3 || number(with[1]) != want {
			t.Errorf("%s's score %+v, want %d", holder, with, want)
		}
	}

	c.ChatCommand("execute store result score @s points run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	expectScore("Testificate", 7)
	c.ChatCommand("execute store success score Alice points run data get storage test:a nope")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "nope"))
	expectScore("Alice", 0)

	c.ChatCommand("data merge storage test:a {x:-4}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("execute store result score * points run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "-4")
	expectScore("Testificate", -4)
	expectScore("Alice", -4)

	c.Disconnect()
}

// TestExecuteStoreScoreRefusals checks the score store's throws: a selector
// naming nobody and a * on an empty board refuse before the run; a
// read-only objective throws only once written, after the run, as
// command.failed, leaving the score it made at zero.
func TestExecuteStoreScoreRefusals(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add points dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("execute store result score * points run say hi")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.scoreHolder.empty"))
	c.ChatCommand("execute store result score @e[type=minecraft:pig] points run say hi")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"))

	c.ChatCommand("scoreboard objectives add hp health")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("data merge storage test:a {x:7}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	c.ChatCommand("execute store result score Alice hp run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	expectScoreboardRefusal(t, c, "command.failed")
	c.ChatCommand("scoreboard players get Alice hp")
	with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
	if len(with) != 3 || number(with[1]) != 0 {
		t.Errorf("Alice's hp %+v, want the 0 the throw left", with)
	}

	c.Disconnect()
}

// TestExecuteStoreIntoABossbarMax checks that the max store moves the
// bar's fill and tells the players on it.
func TestExecuteStoreIntoABossbarMax(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	bar := expectBossBarAdd(t, bob, "The Test Bar", 0)
	alice.ChatCommand("bossbar set test:bar value 50")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", testBarDisplay, servertest.NumberText(50))
	expectBossBarProgress(t, bob, bar, 0.5)
	alice.ChatCommand("data merge storage test:a {x:200}")
	alice.ExpectCommandFeedback("commands.data.storage.modified", "test:a")

	alice.ChatCommand("execute store result bossbar test:bar max run data get storage test:a x")
	alice.ExpectCommandFeedback("commands.data.storage.query", "test:a", "200")
	expectBossBarProgress(t, bob, bar, 0.25)

	// A success stores 1, the max the value then overfills.
	alice.ChatCommand("execute store success bossbar test:bar max run data get storage test:a x")
	alice.ExpectCommandFeedback("commands.data.storage.query", "test:a", "200")
	expectBossBarProgress(t, bob, bar, 1)

	alice.Disconnect()
	bob.Disconnect()
}
