package entities

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBoatCrossesPortals: an unridden boat that a portal takes arrives in the
// other dimension and leaves no entry in its old chunk's index (the harness's
// cleanup checks the index).
// vanilla: Entity.teleportCrossDimension
func TestBoatCrossesPortals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, dim string
		setup     []string
	}{
		{"nether", "the_nether", []string{
			"fill 2 64 1 2 68 4 obsidian",
			"fill 2 65 2 2 67 3 nether_portal[axis=z]",
			"summon oak_boat 2.5 65 2.5 {NoGravity:1b}",
		}},
		{"end", "the_end", []string{
			"setblock 2 63 2 end_portal",
			"summon oak_boat 2.5 63.5 2.5 {NoGravity:1b}",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CrossingTestConfig(t)
			srv, _ := servertest.Join(t, cfg, "Sailor", servertest.OpsListing(t, "Sailor"))
			servertest.HoldTicks(t, srv)
			for _, line := range tc.setup {
				srv.RunLine(line)
			}
			arrived := func() bool {
				out := srv.RunLine("execute as @e[type=oak_boat] at @s if dimension " + tc.dim)
				return strings.Contains(fmt.Sprint(out), "conditional.pass")
			}
			servertest.UntilTicks(t, srv, 400, "the boat to arrive in "+tc.dim, arrived)
			if err := srv.ChunkIndexDrift(); err != nil {
				t.Error(err)
			}
		})
	}
}
