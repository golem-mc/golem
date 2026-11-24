package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBrieflyCoveredDirtPath checks that a dirt path whose cover is gone by
// its scheduled tick still turns to dirt, that farmland rechecks and
// survives, and that a path replaced meanwhile is left alone.
func TestBrieflyCoveredDirtPath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()

	set := func(x int32, name string, props map[string]string) {
		servertest.SetFarmBlock(t, c, srv, x, 63, 0, name, props)
	}
	set(1, "minecraft:dirt_path", nil)
	set(3, "minecraft:farmland", map[string]string{"moisture": "7"})
	set(5, "minecraft:dirt_path", nil)
	for _, x := range []int32{1, 3, 5} {
		servertest.SetFarmBlock(t, c, srv, x, 64, 0, "minecraft:stone", nil)
	}
	for _, x := range []int32{1, 3} {
		servertest.SetFarmBlock(t, c, srv, x, 64, 0, "minecraft:air", nil)
	}
	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:stone", nil)
	c.Settle()
	srv.TickPhysics(1)

	if got := servertest.CropsNameOf(t, srv.BlockAt(1, 63, 0)); got != "minecraft:dirt" {
		t.Errorf("briefly covered path = %s, want minecraft:dirt", got)
	}
	if got := servertest.CropsNameOf(t, srv.BlockAt(3, 63, 0)); got != "minecraft:farmland" {
		t.Errorf("briefly covered farmland = %s, want minecraft:farmland", got)
	}
	if got := servertest.CropsNameOf(t, srv.BlockAt(5, 63, 0)); got != "minecraft:stone" {
		t.Errorf("replaced path = %s, want minecraft:stone", got)
	}
}
