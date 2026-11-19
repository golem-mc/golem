package commands_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFetchProfileEntityBuildsTheLinks runs /fetchprofile entity on the
// runner and on a pig: the player's profile comes with the give link, the pig
// has none.
// vanilla: FetchProfileCommand.printForEntity
func TestFetchProfileEntityBuildsTheLinks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("fetchprofile entity @s")
	data, ok := c.SystemChatContaining(srv, 2, "commands.fetchprofile.entity.success")
	if !ok {
		t.Fatal("/fetchprofile entity @s sent no answer")
	}
	for _, want := range []string{
		"run_command",
		"give @s minecraft:player_head[profile=", "summon minecraft:mannequin ~ ~ ~ {profile:",
		"copy_to_clipboard", "Alice",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("the /fetchprofile entity @s answer lacks %q", want)
		}
	}

	c.ChatCommand("summon minecraft:pig ~ ~ ~")
	c.ChatCommand("fetchprofile entity @e[type=pig,limit=1]")
	if !c.SawSystemChatWithin(srv, 3, "commands.fetchprofile.no_profile") {
		t.Error("/fetchprofile entity on a pig did not fail with commands.fetchprofile.no_profile")
	}
}
