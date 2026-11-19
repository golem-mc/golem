package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntitySelector.getResultLimit, EntityLookup.byId
func TestSelectorLimitKeepsLowestLiveIDs(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Searcher", servertest.OpsListing(t, "Searcher"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	for i := range 5 {
		c.ChatCommand(fmt.Sprintf(`summon marker %d 64 0 {Tags:["m%d"]}`, i+1, i+1))
		c.ExpectSystemChatPastMobs()
	}
	for _, cmd := range []string{
		"kill @e[type=marker,tag=m2]",
		"execute as @e[type=marker,limit=2] run tag @s add picked",
		"execute positioned 0 64 0 as @e[type=marker,limit=1,sort=furthest] run tag @s add far",
		// Filters beyond the cheap head must run before counting a match.
		"execute as @e[type=marker,tag=!picked,distance=..100,limit=1] run tag @s add remaining",
	} {
		c.ChatCommand(cmd)
		c.ExpectSystemChatPastMobs()
		if cmd == "execute as @e[type=marker,limit=2] run tag @s add picked" {
			c.ExpectSystemChatPastMobs()
		}
	}
	for _, tc := range []struct {
		tag, marker string
		want        bool
	}{
		{"picked", "m1", true}, {"picked", "m3", true},
		{"picked", "m4", false}, {"picked", "m5", false},
		{"far", "m5", true}, {"far", "m1", false},
		{"remaining", "m4", true}, {"remaining", "m5", false},
	} {
		c.ChatCommand("execute if entity @e[type=marker,tag=" + tc.tag + ",tag=" + tc.marker + "]")
		got := bytes.Contains(c.ExpectSystemChatPastMobs(), []byte("commands.execute.conditional.pass"))
		if got != tc.want {
			t.Errorf("%s on %s = %v, want %v", tc.tag, tc.marker, got, tc.want)
		}
	}
	// The first two live IDs fail the name predicate; the limit must still fill.
	c.ChatCommand(`data merge entity @e[tag=m5,limit=1] {CustomName:"last"}`)
	c.ExpectSystemChatPastMobs()
	c.ChatCommand(`execute if entity @e[type=marker,name=last,limit=1]`)
	if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.execute.conditional.pass")) {
		t.Fatalf("limit counted entities rejected by the predicate: % X", got)
	}
}

func TestSelectorLimitSnapshotCost(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Searcher", servertest.OpsListing(t, "Searcher"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	srv.RunRconLine("scoreboard objectives add x dummy")
	srv.RunRconLine(`summon marker 0 64 0 {Tags:["t"]}`)
	lines := []string{
		"execute as @e[type=marker,limit=1] run scoreboard players set @s x 1",
		"execute as @e[tag=t,limit=1] run scoreboard players set @s x 1",
	}
	run := func(line string) {
		t.Helper()
		feedback := srv.RunRconLine(line)
		if len(feedback) != 1 || feedback[0].Translate != "commands.scoreboard.players.set.success.single" {
			t.Fatalf("%s: %+v", line, feedback)
		}
	}
	costs := make([]float64, len(lines))
	for i, line := range lines {
		costs[i] = testing.AllocsPerRun(10, func() { run(line) })
	}
	for range 1998 {
		srv.RunRconLine(`summon marker 0 64 0 {Tags:["t"]}`)
	}
	srv.RunRconLine(`summon marker 0 64 0 {Tags:["t"],CustomName:"last"}`)
	for i, line := range lines {
		got := testing.AllocsPerRun(10, func() { run(line) })
		t.Logf("%s: allocations 1 marker = %.0f, 2000 markers = %.0f", line, costs[i], got)
		if got > costs[i]*3 {
			t.Errorf("%s: %.0f allocations beside 2000 markers, %.0f beside one; want within 3x", line, got, costs[i])
		}
	}
	// Fill from beyond the bounded first batch when every early name fails.
	run("execute as @e[type=marker,name=last,limit=1] run scoreboard players set @s x 1")
}

// vanilla: EntityLookup.getEntities iterates players and other bodies together.
func TestSelectorLimitOrdersPlayersWithOtherEntities(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Searcher"))
	connect().Join("Searcher")
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	srv.RunRconLine(`summon marker 0 64 0 {Tags:["eligible"]}`)
	servertest.ReleaseTicks(t, srv)
	connect().Join("Later")
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	srv.RunRconLine("tag Later add eligible")
	srv.RunRconLine("execute as @e[tag=eligible,limit=1] run tag @s add picked")
	got := srv.RunRconLine("execute if entity @e[type=marker,tag=picked]")
	if len(got) != 1 || got[0].Translate != "commands.execute.conditional.pass_count" {
		t.Fatalf("selected a later player ahead of the earlier marker: %+v", got)
	}
}
