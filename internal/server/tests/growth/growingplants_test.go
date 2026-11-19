package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// TestGrowingPlantHeadsGrowOnRandomTicks checks that the kelp and the three
// vines extend one cell at a time, leaving the plant block behind, and stop
// at age 25. The four grow side by side in one server, each in a column of
// its own four blocks from the next, so one run of random ticks grows them
// all.
func TestGrowingPlantHeadsGrowOnRandomTicks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		head, body string
		dy         int32
		fill       string
	}{
		{"minecraft:kelp", "minecraft:kelp_plant", 1, "minecraft:water"},
		{"minecraft:weeping_vines", "minecraft:weeping_vines_plant", -1, "minecraft:air"},
		{"minecraft:twisting_vines", "minecraft:twisting_vines_plant", 1, "minecraft:air"},
		{"minecraft:cave_vines", "minecraft:cave_vines_plant", -1, "minecraft:air"},
	}
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.GrowthTickSpeed)

	const y = 70
	for i, tc := range cases {
		z := int32(i) * 4
		if tc.fill != "minecraft:air" {
			for i := int32(1); i <= 3; i++ {
				servertest.SetFarmBlock(t, c, srv, 5, y+i*tc.dy, z, tc.fill, nil)
			}
		}
		servertest.SetFarmBlock(t, c, srv, 5, y-tc.dy, z, "minecraft:stone", nil)
		servertest.SetFarmBlock(t, c, srv, 5, y, z, tc.head, map[string]string{"age": "23"})
	}

	for range 3000 {
		srv.TickRandomTicks(1)
	}
	for i, tc := range cases {
		z := int32(i) * 4
		t.Run(tc.head, func(t *testing.T) {
			for i := range int32(2) {
				name, props := servertest.StateProps(srv.BlockAt(5, y+i*tc.dy, z))
				if name != tc.body {
					t.Errorf("cell %d stands as %s %v, want %s", i, name, props, tc.body)
				}
			}
			name, props := servertest.StateProps(srv.BlockAt(5, y+2*tc.dy, z))
			if name != tc.head || props["age"] != strconv.Itoa(25) {
				t.Errorf("tip stands as %s %v, want %s at age 25", name, props, tc.head)
			}
			if name, _ := servertest.StateProps(srv.BlockAt(5, y+3*tc.dy, z)); name != tc.fill {
				t.Errorf("the plant grew past age 25: %s", name)
			}
		})
	}
}
