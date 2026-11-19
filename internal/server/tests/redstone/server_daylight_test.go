package redstone

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

// TestADaylightDetectorInTheNetherKeepsItsPowerForTheClick carries the actor
// to the nether and stands a detector there: a dimension without sky light
// keeps no ticker for one, so noon on the shared clock reads nothing and
// nothing rereads it, and the click alone moves the power - the flip
// reading the full fifteen, fifteen less the dark sky's nothing.
// vanilla: DaylightDetectorBlock.getTicker, useWithoutItem
func TestADaylightDetectorInTheNetherKeepsItsPowerForTheClick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	srv.MovePlayerToDimension("Actor", world.NetherName, 80, 40, -17)
	servertest.AwaitViewSent(t, c)

	srv.SetDayTime(6000)
	servertest.Setblock(t, c, 80, 40, -18, "daylight_detector")
	servertest.AwaitTicks(t, srv, 40)

	// No ticker visits it: noon's full sky counts for nothing there, and
	// the power a use last wrote is the one it keeps - the nether's sky
	// light is zero everywhere, so the plain mode reads zero too.
	name, props := servertest.StateProps(srv.BlockInDimension(world.NetherName, 80, 40, -18))
	if name != "minecraft:daylight_detector" || props["power"] != "0" || props["inverted"] != "false" {
		t.Fatalf("the nether's detector = %s[%v], want the plain zero reading", name, props)
	}

	// The click is the only thing that moves its power there.
	c.FacePlayer(80.5, 40, -16.5, 180, 0)
	c.Settle()
	c.UseBlock(80, 40, -18)
	c.Settle()
	name, props = servertest.StateProps(srv.BlockInDimension(world.NetherName, 80, 40, -18))
	if name != "minecraft:daylight_detector" || props["inverted"] != "true" || props["power"] != "15" {
		t.Fatalf("the flipped nether detector = %s[%v], want inverted reading 15", name, props)
	}

	// The clock runs on to noon's far side and the reading still keeps.
	srv.SetDayTime(18000)
	servertest.AwaitTicks(t, srv, 40)
	name, props = servertest.StateProps(srv.BlockInDimension(world.NetherName, 80, 40, -18))
	if name != "minecraft:daylight_detector" || props["inverted"] != "true" || props["power"] != "15" {
		t.Fatalf("the nether detector after midnight = %s[%v], want the flipped reading kept at 15", name, props)
	}
}

// TestAnAdventureHandDoesNotFlipTheDaylightDetector: the use that flips the
// detector is one vanilla answers only to a hand that may build, so an
// adventure click leaves the mode alone.
// vanilla: DaylightDetectorBlock.useWithoutItem
func TestAnAdventureHandDoesNotFlipTheDaylightDetector(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.AdventureScene(t)

	srv.SetBlock(2, 64, 0, registry.BlockStates["minecraft:daylight_detector"])
	actor.UseBlock(2, 64, 0)
	actor.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:daylight_detector",
		map[string]string{"inverted": "false"})
}

// TestAnObserverWatchingADaylightDetectorPulsesOnAPowerChange: the changed
// power is a flag-3 write, whose shape pass asks the neighbours, so an
// observer facing the detector sees the change and pulses, lighting the lamp
// behind its output.
// vanilla: DaylightDetectorBlock.updateSignalStrength, Level.setBlock
func TestAnObserverWatchingADaylightDetectorPulsesOnAPowerChange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 6, "daylight_detector")
	servertest.Setblock(t, c, 7, 64, 6, "observer[facing=west]")
	servertest.Setblock(t, c, 8, 64, 6, "redstone_lamp")

	srv.SetDayTime(18000)
	servertest.AwaitTicks(t, srv, 40)
	if power := detectorPower(t, srv, 6, 64, 6); power != 0 {
		t.Fatalf("setup: the detector read %d at midnight, want 0", power)
	}

	// The rise to 15 runs the shape pass; the observer pulses for two game
	// ticks and lights the lamp behind it.
	srv.SetDayTime(6000)
	servertest.WaitForTicks(t, srv, 80, func() bool { return servertest.LampLitAt(srv, 8, 64, 6) },
		"the lamp behind the observer never lit when the detector's power rose")
}

// TestAPistonOutOfReachOfADaylightDetectorStaysRetracted: the detector has no
// strong power, so the flag-3 write wakes its six direct neighbours only. A
// piston that reads the detector only through the quasi-connectivity ring -
// two steps from the detector - is left retracted until something else
// updates it.
// vanilla: DaylightDetectorBlock.updateSignalStrength, Level.setBlock,
// PistonBaseBlock.getNeighborSignal
func TestAPistonOutOfReachOfADaylightDetectorStaysRetracted(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// The detector at (7,65,6) is on the ring cell above the piston, so the
	// piston reads it through quasi-connectivity and not across a face.
	servertest.Setblock(t, c, 6, 64, 6, "piston[facing=north]")
	servertest.Setblock(t, c, 7, 65, 6, "daylight_detector")

	srv.SetDayTime(18000)
	servertest.AwaitTicks(t, srv, 40)
	srv.SetDayTime(6000)
	servertest.WaitForTicks(t, srv, 80, func() bool { return detectorPower(t, srv, 7, 65, 6) == 15 },
		"the detector never rose to power 15 at noon")
	// Give a piston that did wake the few ticks to finish extending.
	servertest.AwaitTicks(t, srv, 20)

	name, props := servertest.StateProps(srv.BlockAt(6, 64, 6))
	if name != "minecraft:piston" || props["extended"] == "true" {
		t.Fatalf("the piston = %s[%v], want it still retracted", name, props)
	}
}

// TestADaylightDetectorInTheEndIgnoresTheOverworldClock: the end has no day
// timeline, so its sun angle stays 0 and its sky is never darkened. An
// open-sky detector reads 15 at the overworld's midnight and an inverted one
// reads 0.
// vanilla: DaylightDetectorBlock.updateSignalStrength, EnvironmentAttributes.SUN_ANGLE
func TestADaylightDetectorInTheEndIgnoresTheOverworldClock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Actor", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	servertest.AwaitViewSent(t, c)
	servertest.UntilTicks(t, srv, 200, "the end around the player", func() bool {
		return srv.PlayerDimension("Actor") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, arrival.X+2, arrival.Z)
	})

	x, y, z := arrival.X+2, arrival.Y+8, arrival.Z
	plain := registry.BlockStates["minecraft:daylight_detector"]
	if !srv.SetBlockInDimension(world.EndName, x, y, z, plain) {
		t.Fatal("could not place the end detector")
	}
	if !srv.EnsureBlockEntityInDimension(world.EndName, x, y, z, world.KindDaylightDetector) {
		t.Fatal("the end detector has no ticker")
	}
	srv.SetDayTime(18000)
	servertest.AwaitTicks(t, srv, 60)
	name, props := servertest.StateProps(srv.BlockInDimension(world.EndName, x, y, z))
	if name != "minecraft:daylight_detector" || props["power"] != "15" {
		t.Fatalf("the end detector at midnight = %s[%v], want power 15", name, props)
	}

	c.FacePlayer(float64(x)+0.5, float64(y)-1, float64(z)+2.5, 180, 0)
	c.Settle()
	c.UseBlock(x, y, z)
	c.Settle()
	servertest.AwaitTicks(t, srv, 60)
	name, props = servertest.StateProps(srv.BlockInDimension(world.EndName, x, y, z))
	if name != "minecraft:daylight_detector" || props["inverted"] != "true" || props["power"] != "0" {
		t.Fatalf("the inverted end detector at midnight = %s[%v], want power 0", name, props)
	}
}
