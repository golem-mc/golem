package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestUnsupportedWaterPlantsStayUntilANeighbourChanges(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"),
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)

	const y, z = 80, 5
	for i, tc := range []struct {
		block  registry.Block
		fill   bool
		dx, dy int32
	}{
		{block: registry.BlockSeagrass, dx: 1},
		{block: registry.BlockSeaPickle, dx: 1},
		{block: registry.BlockTallSeagrass, dy: 1},
		{block: registry.BlockSeagrass, fill: true, dx: 1},
		{block: registry.BlockSeaPickle, fill: true, dx: 1},
		{block: registry.BlockTallSeagrass, fill: true, dy: 1},
	} {
		command := "setblock"
		if tc.fill {
			command = "fill"
		}
		c.Run(t, command+"/"+tc.block.Name(), func(t *testing.T) {
			x := int32(2 + 4*i)
			if tc.fill {
				c.ChatCommand(fmt.Sprintf("fill %d %d %d %d %d %d %s", x, y, z, x, y, z, tc.block.Name()))
				servertest.AwaitCommandFeedbackText(t, c, "commands.fill.success", servertest.NumberText(1))
			} else {
				servertest.Setblock(t, c, x, y, z, tc.block.Name())
			}
			servertest.RunTicks(t, srv, 10)
			if got := registry.BlockOf(srv.BlockAt(x, y, z)); got != tc.block {
				t.Errorf("plant after 10 ticks = %s, want %s", got.Name(), tc.block.Name())
			}
			servertest.ExpectBlockNamed(t, srv, x, y-1, z, "minecraft:air", nil)
			// Tall seagrass checks its partner when the block above changes.
			servertest.Setblock(t, c, x+tc.dx, y+tc.dy, z, "stone")
			if registry.BlockOf(srv.BlockAt(x, y, z)) == tc.block {
				t.Errorf("%s survived the neighbour change", tc.block.Name())
			}
		})
	}
}
