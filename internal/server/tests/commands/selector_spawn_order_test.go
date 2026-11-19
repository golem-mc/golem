package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSelectorTiesFollowSpawnOrder checks that an unsorted, nearest or
// furthest selector over entities at one spot names the first-spawned one.
// vanilla: EntityLookup.byId, EntitySelector.sortAndLimit
func TestSelectorTiesFollowSpawnOrder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()
	servertest.FreezeUntilHeld(t, srv)

	for _, name := range []string{"A", "B", "C", "D", "E"} {
		c.ChatCommand(`summon husk 12.5 64 12.5 {NoAI:1b,CustomName:"` + name + `"}`)
		c.ExpectSystemChatPastMobs()
	}
	pass := func(cmd string) bool {
		c.ChatCommand(cmd)
		return bytes.Contains(c.ExpectSystemChatPastMobs(), []byte("commands.execute.conditional.pass"))
	}
	for _, sel := range []string{"limit=1", "limit=1,sort=furthest", "limit=1,sort=nearest"} {
		for i := range 20 {
			c.ChatCommand("tag @e[type=husk," + sel + "] add pick")
			c.ExpectSystemChatPastMobs()
			if i == 19 && (!pass("execute if entity @e[type=husk,tag=pick,name=A]") || pass("execute if entity @e[type=husk,tag=pick,name=!A]")) {
				t.Fatalf("%s: tagged a husk other than the first-spawned one", sel)
			}
		}
	}
	c.ChatCommand("kill @e[type=husk,limit=2]")
	c.ExpectSystemChatPastMobs()
	for name, want := range map[string]bool{"A": false, "B": false, "C": true, "D": true, "E": true} {
		if got := pass("execute if entity @e[type=husk,name=" + name + "]"); got != want {
			t.Errorf("husk %s alive = %v, want %v", name, got, want)
		}
	}
}
