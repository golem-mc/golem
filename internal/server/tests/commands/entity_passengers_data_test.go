package commands

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.saveWithoutId, EntityDataAccessor.getData, NbtContents.resolve
func TestMobPassengersInEntityData(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`tick freeze`)
	servertest.AwaitSystemChatSaying(t, c, "commands.tick.status.frozen")
	c.Settle()
	c.ChatCommand(`summon pig 8 68 7 {Tags:["query_vehicle"],NoAI:1b,PersistenceRequired:1b,Passengers:[{id:"minecraft:cow",UUID:[I;1,2,3,4],NoAI:1b,PersistenceRequired:1b,Passengers:[{id:"minecraft:chicken",UUID:[I;5,6,7,8],NoAI:1b,PersistenceRequired:1b}]}]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.Settle()
	const selector = "@e[type=pig,tag=query_vehicle,limit=1]"
	check := func(path, want string) {
		t.Helper()
		c.ChatCommand("data get entity " + selector + " " + path)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
			t.Fatalf("%s: query = %+v", path, line)
		}
		if got := line.With[1].Plain(); got != want {
			t.Errorf("%s: data get = %q, want %q", path, got, want)
		}
		for _, plain := range []bool{false, true} {
			c.ChatCommand(fmt.Sprintf(`tellraw @s {nbt:%q,entity:%q,plain:%t}`, path, selector, plain))
			text := servertest.FeedbackComponent(t, c)
			if got := text.Plain(); got != want {
				t.Errorf("%s (plain=%t): text = %q, want %q", path, plain, got, want)
			}
			var coloured func(protocol.Text) bool
			coloured = func(text protocol.Text) bool {
				if text.Color != "" {
					return true
				}
				return slices.ContainsFunc(text.Extra, coloured)
			}
			if got := coloured(text); got == plain {
				t.Errorf("%s (plain=%t): coloured = %t", path, plain, got)
			}
		}
	}
	check("Passengers[0].id", `"minecraft:cow"`)
	check("Passengers[0].UUID", "[I; 1, 2, 3, 4]")
	check("Passengers[0].Passengers[0].id", `"minecraft:chicken"`)
	check("Passengers[0].Passengers[0].UUID", "[I; 5, 6, 7, 8]")

	// The query must read the current rider, not the original summon tag.
	c.ChatCommand(`data merge entity @e[type=cow,limit=1] {Health:7f}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	c.Settle()
	check("Passengers[0].Health", "7.0f")
	c.ChatCommand(`ride @e[type=cow,limit=1] dismount`)
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	c.Settle()
	c.ChatCommand("data get entity " + selector + " Passengers")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "Passengers"))
	c.ChatCommand("data get entity " + selector + " id")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "id"))
}
