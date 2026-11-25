package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestDoublePlantWithoutGroundBreaksOnSideUpdate checks that a tall grass
// standing on stone breaks when a neighbour beside it changes, while one on
// dirt stays.
// vanilla: DoublePlantBlock.updateShape, VegetationBlock.updateShape
func TestDoublePlantWithoutGroundBreaksOnSideUpdate(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()

	for _, tc := range []struct {
		z      int32
		floor  string
		broken bool
	}{{0, "minecraft:stone", true}, {4, "minecraft:dirt", false}} {
		servertest.SetFarmBlock(t, c, srv, 1, 99, tc.z, tc.floor, nil)
		lower := servertest.SetFarmBlock(t, c, srv, 1, 100, tc.z, "minecraft:tall_grass", map[string]string{"half": "lower"})
		servertest.SetFarmBlock(t, c, srv, 1, 101, tc.z, "minecraft:tall_grass", map[string]string{"half": "upper"})
		srv.TickPhysics(2)
		if got := srv.BlockAt(1, 100, tc.z); got != lower {
			t.Fatalf("on %s the grass is %d before the side update, want %d", tc.floor, got, lower)
		}
		servertest.SetFarmBlock(t, c, srv, 2, 100, tc.z, "minecraft:stone", nil)
		srv.TickPhysics(2)
		got := srv.BlockAt(1, 100, tc.z)
		if tc.broken && got != 0 {
			t.Errorf("tall grass on %s after a side update = %d, want air", tc.floor, got)
		}
		if !tc.broken && got != lower {
			t.Errorf("tall grass on %s after a side update = %d, want %d", tc.floor, got, lower)
		}
	}
}
