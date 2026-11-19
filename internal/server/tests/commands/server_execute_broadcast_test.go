package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// An execute as changes the entity of the command source, not whether the
// source informs the operators, so the echo follows the runner's own rule.
// vanilla: CommandSourceStack.broadcastToAdmins

// readsSystemChat settles c and reports whether a System Chat line came in
// ahead of the settle's answer.
func readsSystemChat(c *servertest.Client) (got bool) {
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySystemChat {
			got = true
		}
	})
	return got
}

// TestExecuteAsFromRconFollowsTheBroadcastProperty runs an execute as over an
// RCON that does not broadcast to operators: the operator reads nothing.
func TestExecuteAsFromRconFollowsTheBroadcastProperty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.BroadcastRconToOps = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	srv.RunRconLine("summon armor_stand 0 64 5")
	srv.RunRconLine("execute as @e[type=armor_stand] run tp @s ~ ~ ~")
	if readsSystemChat(alice) {
		t.Error("the operator read an echo of an execute as the source does not broadcast")
	}
}

// TestExecuteAsFromConsoleFollowsTheBroadcastProperty is the same for the
// server console and broadcast-console-to-ops.
func TestExecuteAsFromConsoleFollowsTheBroadcastProperty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.BroadcastConsoleToOps = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	srv.RunLine("summon armor_stand 0 64 5")
	srv.RunLine("execute as @e[type=armor_stand] run tp @s ~ ~ ~")
	if readsSystemChat(alice) {
		t.Error("the operator read an echo of an execute as the source does not broadcast")
	}
}

// TestExecuteAsFromRconStillBroadcastsWhenAllowed pins the other side of the
// property: with broadcast-rcon-to-ops on, the operator reads the echo.
func TestExecuteAsFromRconStillBroadcastsWhenAllowed(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.BroadcastRconToOps = true
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	srv.RunRconLine("summon armor_stand 0 64 5")
	srv.RunRconLine("execute as @e[type=armor_stand] run tp @s ~ ~ ~")
	if !readsSystemChat(alice) {
		t.Fatal("the operator read no echo of an execute as from an allowed RCON")
	}
}

// TestExecuteAsFromCommandBlockFollowsCommandBlockOutput runs a tracking
// block under command_block_output false: no echo for the executing entity,
// and the echo is back once the rule is on again.
func TestExecuteAsFromCommandBlockFollowsCommandBlockOutput(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "command_block[facing=north,conditional=false]")
	servertest.Setblock(t, c, 2, 65, 0, "lever[face=floor,facing=north]")
	c.ChatCommand("summon armor_stand 0 64 5")
	c.ChatCommand("gamerule command_block_output false")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_block_output", "false")
	c.Settle()

	run := func(z int32, cmd string) (echoed bool) {
		c.ProgramCommandBlock(2, 64, 0, cmd, servertest.ProgramModeImpulse, true, false, false)
		servertest.AwaitCommandFeedback(t, c, "advMode.setCommand.success", cmd)
		stone := servertest.SupportState(t, "minecraft:stone", nil)
		c.UseBlock(2, 65, 0)
		collect := func(id int32, _ []byte) bool {
			if id == protocol.IDPlaySystemChat {
				echoed = true
			}
			return false
		}
		for i := 0; i < 40 && srv.BlockAt(5, 64, z) != stone; i++ {
			c.PumpTicks(srv, 1, collect)
		}
		if srv.BlockAt(5, 64, z) != stone {
			t.Fatal("the block never ran its command")
		}
		c.PumpTicks(srv, 2, collect)
		return echoed
	}

	if run(0, "execute as @e[type=armor_stand] run setblock 5 64 0 stone") {
		t.Error("an execute as echoed to the operator with command_block_output false")
	}
	c.ChatCommand("gamerule command_block_output true")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_block_output", "true")
	c.UseBlock(2, 65, 0)
	c.Settle()
	if !run(1, "execute as @e[type=armor_stand] run setblock 5 64 1 stone") {
		t.Error("an execute as did not echo with command_block_output true")
	}
}

// TestExecuteAsFromPlayerEchoesUnderTheEntity has an operator run an execute
// as on themselves: the other operator reads the echo under that name.
func TestExecuteAsFromPlayerEchoesUnderTheEntity(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})

	alice.ChatCommand("execute as @s run give @s stone")
	feedback := stoneGiveFeedback(t, servertest.PlayerDisplayComponent("Alice"))
	servertest.AwaitCommandFeedbackText(t, alice, "commands.give.success.single", feedback.With...)
	alice.Settle()

	servertest.AwaitAdminBroadcast(t, bob, servertest.PlayerDisplayComponent("Alice"), feedback)
	servertest.AwaitNoSystemChat(t, alice)
}

// TestParticlePlaysoundAndNamedSummonEchoToOperators runs each command that
// reports with sendSuccess(.., true): the other operator reads the echo.
// vanilla: ParticleCommand.sendParticles, PlaySoundCommand.playSound, SummonCommand.spawnEntity
func TestParticlePlaysoundAndNamedSummonEchoToOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
	bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ command, key string }{
		{"particle minecraft:flame ~ ~ ~", "commands.particle.success"},
		{"playsound minecraft:entity.pig.ambient master @s", "commands.playsound.success.single"},
		{"summon pig ~ ~ ~ {CustomName:\"x\"}", "commands.summon.success"},
	} {
		alice.ChatCommand(tc.command)
		alice.Settle()
		// The arguments are not compared; the line is recognised by the
		// two translation keys.
		if !bob.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			return id == protocol.IDPlaySystemChat &&
				bytes.Contains(data, []byte("chat.type.admin")) && bytes.Contains(data, []byte(tc.key))
		}) {
			t.Errorf("no admin echo of %s arrived", tc.key)
		}
	}
}
