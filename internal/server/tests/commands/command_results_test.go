package commands_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandsResultIsTheirCount checks that /execute store result takes the
// count a command answers rather than 1: the entities killed, the blocks
// filled and tagged, the players given an item.
// vanilla: KillCommand.kill, FillCommand.fillBlocks, TagCommand.addTag, GiveCommand.giveItem
func TestCommandsResultIsTheirCount(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	for range 3 {
		c.ChatCommand("summon minecraft:cow 2.5 70 2.5")
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.ExpectCommandFeedbackText("commands.summon.success",
			servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}

	c.ChatCommand("execute store result storage t:a t int 1 run tag @e[type=minecraft:cow] add x")
	c.ExpectCommandFeedbackText("commands.tag.add.success.multiple", protocol.Text{Text: "x"}, servertest.NumberText(3))
	c.ChatCommand("data get storage t:a t")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "3")

	c.ChatCommand("execute store result storage t:a k int 1 run kill @e[type=minecraft:cow]")
	c.ExpectCommandFeedbackText("commands.kill.success.multiple", servertest.NumberText(3))
	c.ChatCommand("data get storage t:a k")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "3")

	c.ChatCommand("execute store result storage t:a f int 1 run fill 0 70 0 1 70 1 minecraft:stone")
	c.ExpectCommandFeedbackText("commands.fill.success", servertest.NumberText(4))
	c.ChatCommand("data get storage t:a f")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "4")
}

// TestTeamTitleAndSoundCommandsReturnTheirCountToExecuteStore checks the
// counts /team, /title, /playsound, /stopsound, /particle and /spawnpoint
// answer /execute store with: a team count, a member count, 0 for a team
// option but prefix and suffix, and the number of players acted on.
// vanilla: TeamCommand, TitleCommand, PlaySoundCommand, StopSoundCommand,
// ParticleCommand, SetSpawnCommand
func TestTeamTitleAndSoundCommandsReturnTheirCountToExecuteStore(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	feedback := func(command, key string) protocol.Text {
		t.Helper()
		c.ChatCommand(command)
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlaySystemChat))
		got, ok := protocol.ReadText(r)
		if !ok || got.Translate != key {
			t.Fatalf("/%s answered %+v, want %q", command, got, key)
		}
		return got
	}
	feedback("scoreboard objectives add r dummy", "commands.scoreboard.objectives.add.success")
	feedback("team add red", "commands.team.add.success")
	feedback("team add blue", "commands.team.add.success")

	for _, tc := range []struct {
		command, key string
		want         int64
	}{
		{"team list", "commands.team.list.teams.success", 2},
		{"team add green", "commands.team.add.success", 3},
		{"team remove green", "commands.team.remove.success", 2},
		{"team join red @s", "commands.team.join.success.single", 1},
		{"team list red", "commands.team.list.members.success", 1},
		{"team modify red color blue", "commands.team.option.color.success", 0},
		{`team modify red prefix "x"`, "commands.team.option.prefix.success", 1},
		{"team leave @s", "commands.team.leave.success.single", 1},
		{"title @a clear", "commands.title.cleared.single", 1},
		{"playsound minecraft:block.note_block.harp master @a", "commands.playsound.success.single", 1},
		{"stopsound @a", "commands.stopsound.success.sourceless.any", 1},
		{"particle minecraft:flame ~ ~ ~", "commands.particle.success", 1},
		{"spawnpoint @a", "commands.spawnpoint.success.single", 1},
	} {
		feedback("scoreboard players set #r r -1", "commands.scoreboard.players.set.success.single")
		feedback("execute store result score #r r run "+tc.command, tc.key)
		with := feedback("scoreboard players get #r r", "commands.scoreboard.players.get.success").With
		if len(with) != 3 || with[1].Number == nil || *with[1].Number != tc.want {
			t.Errorf("/%s stored %+v, want %d", tc.command, with, tc.want)
		}
	}
}
