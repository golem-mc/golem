package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// The observer watching a dropper or a dispenser: powering the container
// flips its triggered state, and the observer sees the state change on the
// watched face and pulses. A floor lever on the container's roof powers it,
// and the observer's output wire runs back to a lamp behind it. The tick loop
// is stopped and driven by hand, so the two-tick pulse is checked at each
// tick. vanilla: DispenserBlock.neighborChanged, ObserverBlock.updateShape.

// containerObserverScene stands the container at (0, 64, 4) with a floor lever
// on its roof, an observer at (0, 64, 3) facing south to watch it, and the
// observer's output wire and lamp behind it at (0, 64, 2) and (0, 64, 1). The
// flat world's ground stands at y=63 and the player at (0.5, 64, 0.5), inside
// the lever's reach.
func containerObserverScene(t *testing.T, block string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 0, 64, 4, block+"[facing=north]")
	servertest.Setblock(t, c, 0, 65, 4, "lever[face=floor,facing=north]")
	servertest.Setblock(t, c, 0, 64, 2, "redstone_wire[north=side,south=side]")
	servertest.Setblock(t, c, 0, 64, 1, "redstone_lamp")
	// The observer last: placing the container first keeps its shape update
	// from booking a pulse on the observer before the scene is ready.
	servertest.Setblock(t, c, 0, 64, 3, "observer[facing=south]")
	c.Settle()

	// Run the placement wake out; the observer must start dark.
	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 0, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	return srv, c
}

// followContainerObserverPulse pulls the lever and follows the observer for
// twelve ticks, reporting whether it ever lit and whether the lamp behind it
// was seen lit and later dark.
func followContainerObserverPulse(t *testing.T, srv *server.Server, c *servertest.Client) (sawPulse, lampOn, lampOff bool) {
	t.Helper()
	c.UseBlock(0, 65, 4)
	c.Settle()
	for range 12 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 0, 64, 3); powered {
			sawPulse = true
		}
		if servertest.LampLitAt(srv, 0, 64, 1) {
			lampOn = true
		} else if lampOn {
			lampOff = true
		}
	}
	return sawPulse, lampOn, lampOff
}

// TestPoweringAContainerPulsesTheObserverFacingIt pulls the lever on a
// dropper's and a dispenser's roof: the container's triggered state flips, an
// observer watching it pulses, and the lamp behind the observer lights and
// goes dark. Pulling the lever again flips the state back and the observer
// pulses a second time.
func TestPoweringAContainerPulsesTheObserverFacingIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, block string }{
		{"dropper", "minecraft:dropper"},
		{"dispenser", "minecraft:dispenser"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := containerObserverScene(t, tc.block)
			sawPulse, lampOn, lampOff := followContainerObserverPulse(t, srv, c)
			if !sawPulse {
				t.Error("powering the container never pulsed the observer watching it")
			}
			if !lampOn || !lampOff {
				t.Errorf("the lamp behind the observer lit=%v dark=%v, want it to light and go dark", lampOn, lampOff)
			}

			sawPulse, lampOn, lampOff = followContainerObserverPulse(t, srv, c)
			if !sawPulse {
				t.Error("unpowering the container never pulsed the observer watching it")
			}
			if !lampOn || !lampOff {
				t.Errorf("the lamp behind the observer lit=%v dark=%v on the unpower edge", lampOn, lampOff)
			}
		})
	}
}
