package redstone

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BlockInput.place, ObserverBlock.updateShape
func TestCommandPlacedObserverPulses(t *testing.T) {
	for _, tc := range []struct {
		command string
		key     string
		args    []string
	}{
		{"setblock 5 64 5", "commands.setblock.success", []string{"5", "64", "5"}},
		{"fill 5 64 5 5 64 5", "commands.fill.success", []string{"1"}},
	} {
		for _, mode := range []string{"", " strict"} {
			t.Run(tc.command+mode, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor",
					servertest.OpsListing(t, "Actor"), func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
				servertest.HoldTicks(t, srv)
				// Drain the join's chunk tick handoffs before booking the pulse.
				servertest.RunTicks(t, srv, 1)
				servertest.Setblock(t, c, 5, 64, 6, "redstone_lamp")
				for attempt := range 2 {
					c.ChatCommand(tc.command + " observer[facing=north]" + mode)
					if attempt > 0 {
						servertest.AwaitCommandError(t, c, strings.Replace(tc.key, "success", "failed", 1))
					} else if tc.key == "commands.fill.success" {
						servertest.AwaitCommandFeedbackText(t, c, tc.key, servertest.NumberText(1))
					} else {
						servertest.AwaitCommandFeedback(t, c, tc.key, tc.args...)
					}
					poweredTicks, firstPowered := 0, 0
					lampOn, lampOff := false, false
					for tick := 0; tick <= 10; tick++ {
						if tick > 0 {
							servertest.RunTicks(t, srv, 1)
						}
						_, powered := servertest.ObserverPowerAt(srv, 5, 64, 5)
						if powered {
							poweredTicks++
							if firstPowered == 0 {
								firstPowered = tick
							}
						}
						if servertest.LampLitAt(srv, 5, 64, 6) {
							lampOn = true
						} else if lampOn {
							lampOff = true
						}
					}
					if mode != "" {
						if poweredTicks != 0 || lampOn {
							t.Fatalf("strict placement powered %d ticks, lamp lit=%v", poweredTicks, lampOn)
						}
						continue
					}
					if firstPowered < 2 || firstPowered > 3 || poweredTicks != 2 || !lampOn || !lampOff {
						t.Fatalf("first powered tick=%d, powered ticks=%d, lamp lit=%v then dark=%v; want rise at tick 2..3, two-tick pulse and lamp flash", firstPowered, poweredTicks, lampOn, lampOff)
					}
				}
			})
		}
	}
}
