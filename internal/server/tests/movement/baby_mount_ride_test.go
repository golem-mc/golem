package movement

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Wolf.BABY_DIMENSIONS, Turtle.BABY_DIMENSIONS, Llama.BABY_DIMENSIONS, Entity.positionRider
func TestBabyMountPassengerAttachments(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)

	for i, tc := range []struct {
		mount         string
		height, depth float64
	}{
		{"wolf", 0.4375, 0},
		{"turtle", 0.12, 0.075},
		{"llama", 0.81, 0.15},
		{"trader_llama", 0.81, 0.15},
	} {
		t.Run(tc.mount, func(t *testing.T) {
			x, z := 6.5+2*float64(i), 6.5
			c.ChatCommand(fmt.Sprintf(`summon %s %g 64 %g {Age:-24000,NoAI:1b,NoGravity:1b,Rotation:[0f,0f],Passengers:[{id:"minecraft:pig",NoAI:1b,NoGravity:1b}]}`, tc.mount, x, z))
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			checkSeat := func(yaw int, seatX, seatZ float64) {
				t.Helper()
				servertest.RunTicks(t, srv, 1)
				line := fmt.Sprintf("execute if entity @e[type=pig,x=%g,y=%g,z=%g,distance=..0.001]", seatX, 64+tc.height, seatZ)
				if !runHolds(srv, line) {
					t.Errorf("yaw %d: %s missed the pig", yaw, line)
				}
			}
			checkSeat(0, x, z-tc.depth)
			c.ChatCommand("rotate @e[type=" + tc.mount + ",limit=1] 90 0")
			servertest.AwaitSystemChatSaying(t, c, "commands.rotate.success")
			checkSeat(90, x+tc.depth, z)
		})
	}
}
