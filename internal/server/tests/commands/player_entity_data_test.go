package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestExecuteIfPlayerDataFindsRootVehicle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Rider")
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`summon minecraft:pig 2.5 64 0.5 {Tags:["ride_test"],NoAI:1b}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("ride @s mount @e[tag=ride_test,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")

	const condition = `execute if data entity @s RootVehicle.Entity{id:"minecraft:pig"}`
	c.ChatCommand(condition)
	servertest.CheckCommandFeedback(t, c.NextSystemChat(), "commands.execute.conditional.pass_count", servertest.NumberText(1))

	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	c.ChatCommand(condition)
	if got := c.NextSystemChat(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		line, _ := protocol.ReadText(protocol.NewReader(got))
		t.Fatalf("after dismount, %q answered %s, want conditional failure", condition, line.Plain())
	}
}

func TestExecuteIfPlayerDataFindsFlyingEnderPearl(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Thrower")
	servertest.HoldTicks(t, srv)
	servertest.GivePearls(t, srv)

	c.MoveTo(0.5, 64, 0.5, 0, -60)
	servertest.UseHeld(c, 0, -60)
	if pearl := c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl); pearl.Vy <= 0 {
		t.Fatalf("ender pearl vertical velocity = %v, want an upward throw", pearl.Vy)
	}

	c.ChatCommand("execute if data entity @s ender_pearls[0]")
	servertest.CheckCommandFeedback(t, c.NextSystemChat(), "commands.execute.conditional.pass_count", servertest.NumberText(1))
}
