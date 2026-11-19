package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// expectSwing reads the Swing Animation packet, holds it to the default
// animation and returns the entity and the hand it carries.
func expectSwing(t *testing.T, c *servertest.Client) (int32, int32) {
	t.Helper()
	// vanilla: SwingAnimation.DEFAULT, a whack (type 1) of six ticks
	return expectSwingOf(t, c, 1, 6)
}

// expectSwingOf reads the Swing Animation packet, holds it to the animation
// type and duration given and returns the entity and the hand it carries.
func expectSwingOf(t *testing.T, c *servertest.Client, wantKind, wantTicks int32) (int32, int32) {
	t.Helper()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySwingAnimation)
	if !ok {
		t.Fatal("no swing animation arrived")
	}
	r := protocol.NewReader(data)
	id, hand := r.ReadVarInt(), r.ReadVarInt()
	if kind, ticks := r.ReadVarInt(), r.ReadVarInt(); kind != wantKind || ticks != wantTicks || r.Err() != nil || r.Remaining() != 0 {
		t.Errorf("swing = type %d over %d ticks, want type %d over %d and nothing more", kind, ticks, wantKind, wantTicks)
	}
	return id, hand
}

// TestSwingPlaysTheArmAnimation checks that /swing on a living body
// broadcasts the main hand or off hand swing and counts it, and that a
// body that is not living is refused.
func TestSwingPlaysTheArmAnimation(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Swinger", servertest.OpsListing(t, "Swinger"))
	srv.StopTick()

	c.ChatCommand("summon minecraft:armor_stand 1.5 64 0.5")
	stand := c.ExpectArmorStandSpawn()
	name := servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", stand.Identity)

	c.ChatCommand("swing @e[type=minecraft:armor_stand]")
	if id, hand := expectSwing(t, c); id != stand.EntityID || hand != protocol.HandMain {
		t.Errorf("swing = entity %d hand %d, want %d and %d", id, hand, stand.EntityID, protocol.HandMain)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.swing.success.single", name)

	c.ChatCommand("swing @e[type=minecraft:armor_stand] offhand")
	if id, hand := expectSwing(t, c); id != stand.EntityID || hand != protocol.HandOff {
		t.Errorf("offhand swing = entity %d hand %d, want %d and %d", id, hand, stand.EntityID, protocol.HandOff)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.swing.success.single", name)

	// Bare, the runner swings.
	c.ChatCommand("swing")
	if _, hand := expectSwing(t, c); hand != protocol.HandMain {
		t.Errorf("own swing hand = %d, want main hand", hand)
	}

	c.ChatCommand(`summon minecraft:item 6.5 64 0.5 {Item:{id:"minecraft:stone",count:1}}`)
	c.ChatCommand("swing @e[type=minecraft:item]")
	servertest.AwaitCommandError(t, c, "commands.swing.failed.notliving")
	c.Disconnect()
}

// TestSwingAnimationAndDuration checks that /swing sends the animation type
// and duration it names, the default duration when it names only a type, and
// refuses a duration under a tick or a type it does not know.
func TestSwingAnimationAndDuration(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Stabber", servertest.OpsListing(t, "Stabber"))
	name := servertest.PlayerDisplayComponent("Stabber")

	c.ChatCommand("swing @s mainhand stab 12")
	if id, hand := expectSwingOf(t, c, 2, 12); id != c.PlayerEntityID || hand != protocol.HandMain {
		t.Errorf("stab = entity %d hand %d, want %d and %d", id, hand, c.PlayerEntityID, protocol.HandMain)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.swing.success.single", name)
	c.ChatCommand("swing @s offhand none")
	if _, hand := expectSwingOf(t, c, 0, 6); hand != protocol.HandOff {
		t.Errorf("none hand = %d, want off hand", hand)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.swing.success.single", name)
	c.ChatCommand("swing @s mainhand whack 1s")
	expectSwingOf(t, c, 1, 20)
	servertest.AwaitCommandFeedbackText(t, c, "commands.swing.success.single", name)

	c.ChatCommand("swing @s mainhand stab 0")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.time.tick_count_too_low", "1", "0"),
		servertest.CommandErrorContextForTest("swing @s mainhand stab 0", len("swing @s mainhand stab 0")))
	c.ChatCommand("swing @s mainhand poke")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.swing_animation.invalid", "poke"),
		servertest.CommandErrorContextForTest("swing @s mainhand poke", len("swing @s mainhand poke")))
	c.Disconnect()
}

// TestSwingCommandTree checks the swing subtree the client is sent against
// 26.3's commands.json: names, parsers, executable nodes and the duration's
// floor of one tick.
func TestSwingCommandTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	c.SkipJoin()

	parsers := map[int32]string{6: "entity", 43: "time", 60: "swing_animation"}
	var describe func(i int32, depth int) string
	describe = func(i int32, depth int) string {
		n := nodes[i]
		line := strings.Repeat(" ", depth) + n.Name
		if n.Typ == 2 {
			line += " <" + parsers[n.Parser] + ">"
		}
		if n.Parser == 43 {
			line += " min=" + strconv.Itoa(int(n.TimeMin))
		}
		if n.Executable {
			line += " exec"
		}
		line += "\n"
		for _, child := range n.Children {
			line += describe(child, depth+1)
		}
		return line
	}
	got := ""
	for _, i := range nodes[0].Children {
		if nodes[i].Name == "swing" {
			got = describe(i, 0)
		}
	}
	want := `swing exec
 targets <entity> exec
  mainhand exec
   animation <swing_animation> exec
    duration <time> min=1 exec
  offhand exec
   animation <swing_animation> exec
    duration <time> min=1 exec
`
	if got != want {
		t.Errorf("swing tree =\n%s\nwant\n%s", got, want)
	}
	c.Disconnect()
}
