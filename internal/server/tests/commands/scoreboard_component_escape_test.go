package commands_test

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestScoreboardDisplayNameTakesSNBTEscapes checks that a display name written
// with a valid SNBT escape is accepted.
// vanilla: ComponentArgument, SnbtGrammar
func TestScoreboardDisplayNameTakesSNBTEscapes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	servertest.HoldTicks(t, srv)

	c.ChatCommand(`scoreboard objectives add escaped dummy {text:"\u0041"}`)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
	got, ok := protocol.ReadText(r)
	if !ok || got.Translate != "commands.scoreboard.objectives.add.success" {
		t.Fatalf("answered %+v, want the add success", got)
	}
	if name := got.Plain(); !strings.Contains(name, "[A]") {
		t.Fatalf("feedback %q, want the display name [A]", name)
	}
}
