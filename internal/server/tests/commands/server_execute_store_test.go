package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The /execute store kinds that write NBT, each handed the return value or
// the success of the command the chain ends in.
// vanilla: ExecuteCommand.storeData

func TestExecuteStoreRejectsMalformedNBTArguments(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		name   string
		before string
		after  string
		key    string
		with   []string
	}{
		{"scale suffix", "execute store result storage t:a y int 2.5", "x run say hi", "command.expected.separator", nil},
		{"scale exponent", "execute store result storage t:a y int 1.5", "e2 run say hi", "command.expected.separator", nil},
		{"entity scale", "execute store result entity @s Air short 1", "x run say hi", "command.expected.separator", nil},
		{"success scale", "execute store success storage t:a y int 2.5", "x run say hi", "command.expected.separator", nil},
		{"block scale before target lookup", "execute store result block 0 -60 0 x int 1", "x run say hi", "command.expected.separator", nil},
		{"missing list index", "execute store result storage t:a y[", " int 1 run say hi", "parsing.int.expected", nil},
		{"fractional list index", "execute store result storage t:a y[", "1.5] int 1 run say hi", "parsing.int.invalid", []string{"1.5"}},
		{"missing scale before run", "execute store result storage t:a y int ", "run say hi", "parsing.double.expected", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.before + tc.after
			c.ChatCommand(cmd)
			var messages [][]byte
			c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlaySystemChat:
					messages = append(messages, data)
				case protocol.IDPlayDisguisedChat:
					t.Error("say hi ran after a malformed store argument")
				}
				return false
			})
			want := [][]byte{
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(cmd, len(tc.before)),
			}
			if len(messages) != len(want) {
				t.Fatalf("got %d system chat lines, want 2 (error %s and cursor before %q)", len(messages), tc.key, tc.after)
			}
			for i, msg := range messages {
				if !bytes.Equal(msg, want[i]) {
					t.Errorf("chat line %d = % X, want % X", i+1, msg, want[i])
				}
			}
		})
	}
	c.Disconnect()
}

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

// TestExecuteStoreWritesANestedStoragePathFromTwoContexts checks that each
// context's write lands on the live storage and creates the missing parents.
func TestExecuteStoreWritesANestedStoragePathFromTwoContexts(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage test:a {x:7,keep:1b}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:a")
	for range 2 {
		c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}
	c.ChatCommand("execute as @e[type=minecraft:cow] store result storage test:a a.b int 2 run data get storage test:a x")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "7")
	c.ChatCommand("data get storage test:a a.b")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "14")
	c.ChatCommand("data get storage test:a keep")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:a", "1b")

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
