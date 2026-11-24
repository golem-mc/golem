package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBrieflyCoveredDirtPath checks that a dirt path whose cover is gone by
// its scheduled tick still turns to dirt, that farmland rechecks and
// survives, that a path replaced meanwhile is left alone, and that a path
// that replaced a covered farmland does not take the farmland's visit.
func TestBrieflyCoveredDirtPath(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Camper", servertest.OpsListing(t, "Camper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	set := func(x, y int32, name string, props map[string]string) {
		servertest.SetFarmBlock(t, c, srv, x, y, 0, name, props)
	}
	set(1, 63, "minecraft:dirt_path", nil)
	set(3, 63, "minecraft:farmland", map[string]string{"moisture": "7"})
	set(5, 63, "minecraft:dirt_path", nil)
	set(7, 63, "minecraft:farmland", map[string]string{"moisture": "7"})
	for _, x := range []int32{1, 3, 5, 7} {
		set(x, 64, "minecraft:stone", nil)
	}
	for _, x := range []int32{1, 3, 7} {
		set(x, 64, "minecraft:air", nil)
	}
	set(5, 63, "minecraft:stone", nil)
	set(7, 63, "minecraft:dirt_path", nil)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)

	if got := servertest.CropsNameOf(t, srv.BlockAt(1, 63, 0)); got != "minecraft:dirt" {
		t.Errorf("briefly covered path = %s, want minecraft:dirt", got)
	}
	if got := servertest.CropsNameOf(t, srv.BlockAt(3, 63, 0)); got != "minecraft:farmland" {
		t.Errorf("briefly covered farmland = %s, want minecraft:farmland", got)
	}
	if got := servertest.CropsNameOf(t, srv.BlockAt(5, 63, 0)); got != "minecraft:stone" {
		t.Errorf("replaced path = %s, want minecraft:stone", got)
	}
	if got := servertest.CropsNameOf(t, srv.BlockAt(7, 63, 0)); got != "minecraft:dirt_path" {
		t.Errorf("path replacing covered farmland = %s, want minecraft:dirt_path", got)
	}
}
