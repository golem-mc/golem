package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: CommonButtonData.MAP_CODEC, ExtraCodecs.intRange, NbtOps.getNumberValue
func TestDialogButtonWidthDiagnostics(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	for _, kind := range []string{"multi_action", "notice"} {
		for _, tc := range []struct{ name, width, message string }{
			{"zero", "0", "Value must be within range [1;1024]: 0"},
			{"negative", "-1b", "Value must be within range [1;1024]: -1"},
			{"too_wide", "1025", "Value must be within range [1;1024]: 1025"},
			{"fraction", "1025.9d", "Value must be within range [1;1024]: 1025"},
			{"long", "4294967296L", "Value must be within range [1;1024]: 0"},
			{"string", `"bad"`, "Not a number"},
			{"compound", "{}", "Not a number"},
		} {
			c.Run(t, kind+"/"+tc.name, func(t *testing.T) {
				button := `{label:"ok",width:` + tc.width + `}`
				field := `action:` + button
				if kind == "multi_action" {
					field = `actions:[` + button + `]`
				}
				input := `dialog show @s {type:"` + kind + `",title:"x",` + field + `}`
				c.ChatCommand(input)
				c.ExpectCommandError(
					servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.message),
					servertest.CommandErrorContextForTest(input, len(input)),
				)
				c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayShowDialog))
			})
		}
	}
	c.Disconnect()
}
