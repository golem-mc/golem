package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: MultiActionDialog.MAP_CODEC, ExtraCodecs.nonEmptyList, ListCodec.decode
func TestDialogActionErrorsValidatePartialList(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ name, actions, message string }{
		{"empty", `[]`, "List must have contents"},
		{"missing_label", `[{width:5}]`, "No key label in MapLike[{width:5}]; List must have contents"},
		{"all_invalid", `[{width:5},{width:6}]`, "No key label in MapLike[{width:6}]; No key label in MapLike[{width:5}]; List must have contents"},
		{"valid_last", `[{width:5},{label:"ok"}]`, "No key label in MapLike[{width:5}]"},
		{"valid_first", `[{label:"ok"},{width:5}]`, "No key label in MapLike[{width:5}]"},
		{"partial_label_extra", `[{label:{text:"ok",extra:5}}]`, "Not a list: 5"},
		{"empty_label_extra", `[{label:{text:"ok",extra:[]}}]`, "List must have contents"},
		{"nested_label_extra", `[{label:{text:"ok",extra:[{text:"child",extra:5}]}}]`, "Not a list: 5"},
		{"invalid_extra_child", `[{label:{text:"ok",extra:[{nbt:"v"}]}}]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {nbt:"v"}; Second: No matching codec found; List must have contents`},
		{"partial_extra_children", `[{label:{text:"ok",extra:[{nbt:"v"},{text:"child"}]}}]`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {nbt:"v"}; Second: No matching codec found`},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			input := `dialog show @s {type:"multi_action",title:"x",actions:` + tc.actions + `}`
			c.ChatCommand(input)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.message),
				servertest.CommandErrorContextForTest(input, len(input)),
			)
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayShowDialog))
		})
	}
	c.Disconnect()
}
