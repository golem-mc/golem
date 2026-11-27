package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSummonedLeashKnotKeepsTagsAndSilent summons a knot with Tags and
// Silent and finds it by tag, reads Silent back and kills it by name.
// vanilla: Entity.load
func TestSummonedLeashKnotKeepsTagsAndSilent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.Settle()

	srv.RunLine("setblock 40 -60 40 oak_fence")
	srv.RunLine(`summon leash_knot 40 -60 40 {Tags:["k"],Silent:1b,CustomName:"kk"}`)

	var silent string
	for _, text := range srv.RunLine("data get entity @e[tag=k,limit=1] Silent") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			silent = text.With[1].Plain()
		}
	}
	if silent != "1b" {
		t.Errorf("Silent read %q, want 1b", silent)
	}

	killed := false
	for _, text := range srv.RunLine("kill @e[tag=k]") {
		if text.Translate == "commands.kill.success.single" {
			killed = true
		}
	}
	if !killed {
		t.Error("/kill @e[tag=k] did not kill the knot")
	}
	c.Disconnect()
}
