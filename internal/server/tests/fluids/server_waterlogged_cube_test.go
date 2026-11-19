package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FlowingFluid.canPassThroughWall
func TestWaterloggedFullCubesContainWater(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"copper_grate", "oak_leaves", "barrier", "oak_stairs"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, c := waterloggedScene(t)
			props := map[string]string{"waterlogged": "true"}
			if block == "oak_leaves" {
				props["persistent"] = "true"
			}
			servertest.SetFarmBlock(t, c, srv, 10, 66, 0, "minecraft:"+block, props)
			servertest.HoldTicks(t, srv)
			servertest.RunTicks(t, srv, 15)

			if block == "oak_stairs" {
				expectAirAt(t, srv, 10, 65, 0, "below the stairs' closed floor")
				expectWaterAt(t, srv, 10, 66, 1, "1", "beside the stairs' low half")
				expectWaterAt(t, srv, 10, 65, 1, "8", "below the stairs' spill")
			} else {
				for dx := int32(-1); dx <= 1; dx++ {
					for dy := int32(-1); dy <= 1; dy++ {
						for dz := int32(-1); dz <= 1; dz++ {
							if dx != 0 || dy != 0 || dz != 0 {
								expectAirAt(t, srv, 10+dx, 66+dy, dz, "around the waterlogged cube")
							}
						}
					}
				}
			}
			name, got := servertest.StateProps(srv.BlockAt(10, 66, 0))
			if name != "minecraft:"+block || got["waterlogged"] != "true" {
				t.Errorf("source after 15 ticks = %s %v, want waterlogged %s", name, got, block)
			}
		})
	}
}
