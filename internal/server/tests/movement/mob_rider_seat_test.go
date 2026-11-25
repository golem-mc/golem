package movement

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.positionRider, EntityAttachments.getClamped
func TestMobRiderLiveSeatMatchesVanilla(t *testing.T) {
	t.Parallel()
	// Measured on vanilla 26.3 with execute at the rider to teleport an
	// unmounted marker, then data get entity on the marker.
	for _, tc := range []struct {
		mount         string
		before, after [3]float64
	}{
		{"chicken", [3]float64{6.5, 64.7, 6.4}, [3]float64{9.6, 65.7, 8.5}},
		{"turtle", [3]float64{6.5, 64.55625, 6.25}, [3]float64{9.75, 65.55625, 8.5}},
		{"wolf", [3]float64{6.5, 64.81875, 6.4375}, [3]float64{9.5625, 65.81875, 8.5}},
		{"fox", [3]float64{6.5, 64.6375, 6.25}, [3]float64{9.75, 65.6375, 8.5}},
		{"llama", [3]float64{6.5, 65.37, 6.2}, [3]float64{9.8, 66.37, 8.5}},
		{"happy_ghast", [3]float64{6.5, 68, 8.2}, [3]float64{7.8, 69, 8.5}},
	} {
		t.Run(tc.mount, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)

			c.ChatCommand("summon " + tc.mount + ` 6.5 64 6.5 {Age:0,NoAI:1b,NoGravity:1b,Rotation:[0f,0f],Tags:["seat_mount"],Passengers:[{id:"minecraft:pig",Age:0,NoAI:1b,NoGravity:1b,Tags:["seat_rider"]}]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")

			checkSeat := func(pos [3]float64) {
				t.Helper()
				c.ChatCommand(fmt.Sprintf("execute if entity @e[tag=seat_rider,x=%g,y=%g,z=%g,distance=..0.01]", pos[0], pos[1], pos[2]))
				chat := protocol.NewReader(c.AwaitPlay(protocol.IDPlaySystemChat))
				got, ok := protocol.ReadText(chat)
				if !ok {
					t.Fatalf("decode distance selector feedback: %v", chat.Err())
				}
				if got.Translate != "commands.execute.conditional.pass_count" || len(got.With) != 1 || got.With[0].Plain() != "1" {
					t.Errorf("distance selector at vanilla seat %v returned %s, want one rider within 0.01 blocks", pos, got.Plain())
				}
			}
			servertest.RunTicks(t, srv, 1)
			checkSeat(tc.before)

			c.ChatCommand("tp @e[tag=seat_mount,limit=1] 9.5 65 8.5 90 0")
			servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
			servertest.RunTicks(t, srv, 1)
			checkSeat(tc.after)
		})
	}
}
