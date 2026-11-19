package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DropChances.CODEC keys every EquipmentSlot, body and saddle included.
func TestDropChancesKeepBodyAndSaddle(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	sel := func(tag string) string { return "@e[tag=" + tag + ",limit=1]" }
	c.ChatCommand(`summon zombie 4.5 70 4.5 {NoAI:1b,NoGravity:1b,PersistenceRequired:1b,Tags:["bs"],drop_chances:{body:0.5f,saddle:3f}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	// An invalid value leaves only its own slot at the default; the others stay.
	absent := []string{"neg", "def"}
	kept := map[string]string{"negbody": "saddle: 2.0f", "negsaddle": "body: 2.0f", "huge": "saddle: 2.0f", "hugesaddle": "body: 2.0f"}
	for tag, chances := range map[string]string{
		"neg":        "{feet:-1f}",
		"negbody":    "{saddle:2f,body:-1f}",
		"negsaddle":  "{body:2f,saddle:-0.5f}",
		"huge":       "{saddle:2f,body:1e100d}",
		"hugesaddle": "{body:2f,saddle:1e100d}",
	} {
		c.ChatCommand(`summon zombie 4.5 70 4.5 {NoAI:1b,NoGravity:1b,PersistenceRequired:1b,Tags:["` + tag + `"],drop_chances:` + chances + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	c.ChatCommand(`summon zombie 4.5 70 4.5 {NoAI:1b,NoGravity:1b,PersistenceRequired:1b,Tags:["def"],drop_chances:{body:0.085f}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	check := func(c *servertest.Client) {
		t.Helper()
		c.Settle()
		c.ChatCommand("tellraw @s {nbt:'drop_chances',entity:'" + sel("bs") + "'}")
		if text := servertest.FeedbackComponent(t, c).Plain(); !strings.Contains(text, "body: 0.5f") || !strings.Contains(text, "saddle: 3.0f") {
			t.Errorf("tellraw: got %s", text)
		}
		for tag, want := range kept {
			c.ChatCommand("tellraw @s {nbt:'drop_chances',entity:'" + sel(tag) + "'}")
			if text := servertest.FeedbackComponent(t, c).Plain(); !strings.Contains(text, want) || strings.Count(text, ":") != 1 {
				t.Errorf("%s tellraw: got %s, want only %s", tag, text, want)
			}
			c.ChatCommand("data get entity " + sel(tag) + " drop_chances")
			line := servertest.FeedbackComponent(t, c)
			if len(line.With) != 2 || !strings.Contains(line.With[1].Plain(), want) || strings.Count(line.With[1].Plain(), ":") != 1 {
				t.Errorf("%s: got %+v, want only %s", tag, line, want)
			}
		}
		for _, tag := range append([]string{"bs"}, absent...) {
			c.ChatCommand("data get entity " + sel(tag) + " drop_chances")
			line := servertest.FeedbackComponent(t, c)
			if tag != "bs" {
				// An all-default compound is omitted from the saved entity.
				if len(line.Extra) == 1 {
					line = line.Extra[0]
				}
				if line.Translate != "arguments.nbtpath.nothing_found" {
					t.Errorf("%s: want no drop_chances, got %+v", tag, line)
				}
				continue
			}
			if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
				t.Fatalf("%s: unexpected query: %+v", tag, line)
			}
			got := line.With[1].Plain()
			if !strings.Contains(got, "body: 0.5f") || !strings.Contains(got, "saddle: 3.0f") {
				t.Errorf("%s: got %s", tag, got)
			}
		}
	}
	check(c)
	c.ChatCommand("data modify entity " + sel("bs") + " drop_chances.saddle set value 1f")
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	c.ChatCommand("data get entity " + sel("bs") + " drop_chances.saddle")
	if line := servertest.FeedbackComponent(t, c); !strings.Contains(line.Plain(), "1.0") {
		t.Errorf("saddle after modify: %s", line.Plain())
	}
	c.ChatCommand("data modify entity " + sel("bs") + " drop_chances.saddle set value 3f")
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	back := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "mobs wake", func() bool {
		return len(again.SelectedEntities("minecraft:zombie")) == 1+len(absent)+len(kept)
	})
	check(back)
}
