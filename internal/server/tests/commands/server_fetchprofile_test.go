package commands_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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

// TestFetchProfileNameFindsAnOnlinePlayer runs /fetchprofile name for a
// joined player and for an unknown one.
// vanilla: FetchProfileCommand.resolveName
func TestFetchProfileNameFindsAnOnlinePlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.ChatCommand("fetchprofile name alice")
	if !c.SawSystemChatWithin(srv, 40, "commands.fetchprofile.name.success") {
		t.Error("/fetchprofile name alice did not resolve the online player")
	}
	c.ChatCommand("fetchprofile name Nobody")
	if !c.SawSystemChatWithin(srv, 40, "commands.fetchprofile.name.failure") {
		t.Error("/fetchprofile name Nobody did not fail")
	}
}

// TestFetchProfileAnswersTheConsole runs /fetchprofile name and id on the
// console and over RCON: the answer is in the line's output.
// vanilla: FetchProfileCommand.resolveName, FetchProfileCommand.resolveId
func TestFetchProfileAnswersTheConsole(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Alice")

	success := func(how string, out []protocol.Text, key string) {
		t.Helper()
		if len(out) != 1 || out[0].Translate != key {
			t.Errorf("%s = %+v, want one %s", how, out, key)
		}
	}
	success("console fetchprofile name Alice", srv.RunLine("fetchprofile name Alice"), "commands.fetchprofile.name.success")
	success("rcon fetchprofile name Alice", srv.RunRconLine("fetchprofile name Alice"), "commands.fetchprofile.name.success")
	success("console fetchprofile id", srv.RunLine("fetchprofile id "+protocol.OfflineUUID("Alice").String()), "commands.fetchprofile.id.success")

	out := srv.RunLine("fetchprofile name nobody_here")
	if len(out) != 1 || out[0].Color != protocol.ColorRed || len(out[0].Extra) != 1 ||
		out[0].Extra[0].Translate != "commands.fetchprofile.name.failure" {
		t.Errorf("console fetchprofile name nobody_here = %+v, want one red commands.fetchprofile.name.failure", out)
	}
}
