package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A block written by a command relights its column, whether it emits light
// or covers the sky. vanilla: Level.setBlock -> LightEngine.checkBlock
func TestSetblockRelights(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Tester")

	stone, _ := registry.FindState("minecraft:stone", nil)
	// A sealed stone room over x, z in [1,9], y in [64,67]; the floor at
	// y=63 is the flat world's stone.
	for x := int32(1); x <= 9; x++ {
		for z := int32(1); z <= 9; z++ {
			for y := int32(64); y <= 67; y++ {
				if x != 1 && x != 9 && z != 1 && z != 9 && y != 67 {
					continue
				}
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	srv.RelightAt(4, 4)

	srv.RunRconLine("setblock 3 65 5 light[level=15]")
	servertest.UntilTicks(t, srv, 100, "block light 13 two cells from a light block", func() bool {
		return srv.BlockLightAt(5, 65, 5) == 13
	})
	srv.RunRconLine("setblock 3 65 5 air")
	servertest.UntilTicks(t, srv, 100, "the light to clear", func() bool {
		return srv.BlockLightAt(5, 65, 5) == 0
	})

	if got := srv.SkyLightAt(20, 64, 20); got != 15 {
		t.Fatalf("open cell sky light = %d, want 15", got)
	}
	srv.RunRconLine("setblock 20 66 20 stone")
	servertest.UntilTicks(t, srv, 100, "a roof to lower the sky light", func() bool {
		return srv.SkyLightAt(20, 64, 20) < 15
	})
	srv.RunRconLine("setblock 20 66 20 air")
	servertest.UntilTicks(t, srv, 100, "the sky light to return", func() bool {
		return srv.SkyLightAt(20, 64, 20) == 15
	})
}
