package redstone

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// The daylight detector: the sky light over it against the hour's darkening
// is its power, weighted by the sun's angle, reread every twenty ticks of
// the game time, flipped between day and night readings by a right-click,
// and carried to its neighbours as a redstone signal.
// vanilla: DaylightDetectorBlock

// detectorPower reads the power property of the detector standing at the
// block, -1 where some other block stands there.
func detectorPower(t *testing.T, srv *server.Server, x, y, z int32) int {
	t.Helper()
	name, props := servertest.StateProps(srv.BlockAt(x, y, z))
	if name != "minecraft:daylight_detector" {
		return -1
	}
	power, err := strconv.Atoi(props["power"])
	if err != nil {
		return -1
	}
	return power
}

// TestDaylightDetectorReadsNoonAndMidnight stands one on the ground with a
// lamp beside it: noon reads the full fifteen, lighting the lamp through the
// redstone solver, and midnight - the darkened sky against the sun on the
// other side - reads nothing, and the lamp goes dark. The reading rides the
// ticker's twenty-tick cadence, so the waits are counted in it.
func TestDaylightDetectorReadsNoonAndMidnight(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 6, "daylight_detector")
	servertest.Setblock(t, c, 7, 64, 6, "redstone_lamp")

	// Noon: the sky over the detector is fully bright and the sun overhead.
	srv.SetDayTime(6000)
	servertest.WaitForTicks(t, srv, 40, func() bool { return detectorPower(t, srv, 6, 64, 6) == 15 },
		"the detector never read power 15 at noon")
	// The lamp lights in the same tick, but after the detector's write: a
	// read between the two would see the power without the light.
	servertest.WaitForTicks(t, srv, 1, func() bool {
		_, lamp := servertest.StateProps(srv.BlockAt(7, 64, 6))
		return lamp["lit"] == "true"
	}, "the lamp beside the noon detector never lit")

	// Midnight: the hour darkens the sky and the sun reads from below.
	srv.SetDayTime(18000)
	servertest.WaitForTicks(t, srv, 40, func() bool { return detectorPower(t, srv, 6, 64, 6) == 0 },
		"the detector never read power 0 at midnight")
	// The lamp waits out its four-tick dark delay after the power fell.
	servertest.WaitForTicks(t, srv, 10, func() bool {
		_, lamp := servertest.StateProps(srv.BlockAt(7, 64, 6))
		return lamp["lit"] == "false"
	}, "the lamp beside the dark detector never went out")
}

// TestDaylightDetectorFlipsInvertedOnUse clicks one at noon twice: the
// first flip turns it to the inverted mode, whose noon reading is the full
// sky's shortfall of fifteen - nothing - and the second flips it back with
// the power recomputed by the click itself, before any ticker's visit.
func TestDaylightDetectorFlipsInvertedOnUse(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 6, "daylight_detector")
	srv.SetDayTime(6000)
	servertest.WaitForTicks(t, srv, 40, func() bool { return detectorPower(t, srv, 6, 64, 6) == 15 },
		"the detector never read power 15 at noon")

	c.MoveToGround(5.5, 64, 7.5, true)
	c.Settle()
	c.UseBlock(6, 64, 6)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 6, 64, 6, "minecraft:daylight_detector",
		map[string]string{"inverted": "true", "power": "0"})

	c.UseBlock(6, 64, 6)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 6, 64, 6, "minecraft:daylight_detector",
		map[string]string{"inverted": "false", "power": "15"})
}
