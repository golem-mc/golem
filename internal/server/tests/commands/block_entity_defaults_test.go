package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BlockInput.place, StructureBlockEntity.<init>, TestBlockEntity.<init>, JigsawBlockEntity.<init>
func TestCommandBlockEntityPlacementDefaults(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Setter", servertest.OpsListing(t, "Setter"), freeze)
	servertest.HoldTicks(t, srv)

	query := func(t *testing.T, x int32, path, want string) {
		t.Helper()
		line := fmt.Sprintf("data get block %d 63 1", x)
		if path != "" {
			line += " " + path
		}
		if got := plainQuery(t, srv, line, "commands.data.block.query"); got != want {
			t.Errorf("/%s = %s, want %s", line, got, want)
		}
	}

	for i, tc := range []struct{ block, path, want string }{
		{"structure_block", "mode", `"LOAD"`},
		{"structure_block[mode=save]", "mode", `"SAVE"`},
		{"test_block", "mode", `"start"`},
		{"test_block[mode=log]", "mode", `"log"`},
		{"jigsaw", "", `{components: {}, joint: "rollable", name: "minecraft:empty", pool: "minecraft:empty", final_state: "minecraft:air", placement_priority: 0, x: 4, selection_priority: 0, y: 63, z: 1, id: "minecraft:jigsaw", target: "minecraft:empty"}`},
		{"jigsaw[orientation=up_north]", "joint", `"rollable"`},
		{"jigsaw[orientation=down_east]", "joint", `"rollable"`},
		{"structure_block[mode=save]{}", "mode", `"DATA"`},
		{"test_block[mode=log]{}", "mode", `"fail"`},
	} {
		t.Run(tc.block, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf("setblock %d 63 1 %s", i, tc.block))
			c.ExpectCommandFeedback("commands.setblock.success", fmt.Sprint(i), "63", "1")
			query(t, int32(i), tc.path, tc.want)
		})
	}

	t.Run("fill", func(t *testing.T) {
		c.ChatCommand("fill 10 63 1 11 63 1 test_block[mode=log]")
		c.ExpectCommandFeedbackText("commands.fill.success", servertest.NumberText(2))
		for x := int32(10); x <= 11; x++ {
			query(t, x, "mode", `"log"`)
		}
	})
}
