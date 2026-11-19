package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The creaking heart: its block entity's ticker rereads it every 20 to 24
// ticks and the state follows what it finds — pale oak logs on both ends of
// its axis, and the creaking-active part of the overworld's day timeline.
// Uprooted without the logs, awake through that timeline's night, dormant
// the rest of the way.
// vanilla: CreakingHeartBlockEntity.serverTick, updateCreakingState

// creakingHeartState is the heart block state standing upright and carrying
// the named creaking_heart_state, the shape a column of logs holds.
func creakingHeartState(t *testing.T, stateName string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": stateName})
	if !ok {
		t.Fatalf("the registry has no creaking heart %s", stateName)
	}
	return state
}

// creakingHeartStateAt reads the creaking_heart_state of whatever stands at
// the position, the empty string for another block.
func creakingHeartStateAt(srv *server.Server, x, y, z int32) string {
	_, props := servertest.StateProps(srv.BlockAt(x, y, z))
	return props["creaking_heart_state"]
}

// TestACreakingHeartTurnsDormantByDayAndAwakeAtNight stands one awake
// between two logs and watches its ticker carry it to what the hour says:
// dormant across the day the world opens at, awake once the clock reads
// midnight.
func TestACreakingHeartTurnsDormantByDayAndAwakeAtNight(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// Two logs on the heart's axis, and the heart between them arriving
	// awake: the ticker is what has to take it out of that, by the hour.
	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 65, 6, "minecraft:creaking_heart[axis=y,creaking_heart_state=awake]")

	// The day takes the awake heart down to dormant, and the change
	// reaches the wire with it.
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "dormant" },
		"the awake heart never went dormant by day")
	servertest.AwaitBlockUpdate(t, c, 6, 65, 6, creakingHeartState(t, "dormant"))
	servertest.ExpectBlockNamed(t, srv, 6, 65, 6, "minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "dormant"})

	// Midnight wakes it again, the creaking-active stretch of the timeline.
	srv.SetDayTime(18000)
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "awake" },
		"the dormant heart never woke at midnight")
	servertest.ExpectBlockNamed(t, srv, 6, 65, 6, "minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "awake"})
}

// TestACreakingHeartGoesUprootedOnceItsLogsAreGone breaks what holds the
// heart up: a dig takes the log above away, and a log that sits crosswise to
// the heart's axis holds it no better than none at all. An uprooted heart
// stays put: the ticker runs only while the heart sits on its logs, so the
// hour alone moves nothing.
// vanilla: CreakingHeartBlockEntity.updateCreakingState, CreakingHeartBlock.getTicker
func TestACreakingHeartGoesUprootedOnceItsLogsAreGone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	// A creaking linked to a heart keeps it rooted, and an awake heart with
	// its logs stands one up at its first reread: on a loaded machine that
	// comes before the break.
	srv.SetGameRule(world.RuleSpawnMonsters.String(), false)

	srv.SetDayTime(18000)
	// Awake hearts: one column with both logs upright, one whose top log
	// sits crosswise to the heart's axis.
	servertest.Setblock(t, c, 7, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 7, 66, 6, "pale_oak_log")
	servertest.Setblock(t, c, 7, 65, 6, "minecraft:creaking_heart[axis=y,creaking_heart_state=awake]")
	servertest.Setblock(t, c, 11, 64, 3, "pale_oak_log")
	servertest.Setblock(t, c, 11, 66, 3, "pale_oak_log[axis=z]")
	servertest.Setblock(t, c, 11, 65, 3, "minecraft:creaking_heart[axis=y,creaking_heart_state=awake]")

	// A dig of the actor's takes the head of the first column away.
	c.MoveToGround(5.5, 64, 5.5, true)
	c.Settle()
	c.BreakBlock(7, 66, 6)
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 7, 65, 6) == "uprooted" },
		"the heart never went uprooted once the log above it was broken")

	// The crosswise log holds the second heart up no better.
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 11, 65, 3) == "uprooted" },
		"the heart never went uprooted beside a log crosswise to its axis")

	// The hour alone moves an uprooted heart nothing, awake side or not.
	srv.SetDayTime(6000)
	servertest.AwaitTicks(t, srv, 60)
	servertest.ExpectBlockNamed(t, srv, 7, 65, 6, "minecraft:creaking_heart",
		map[string]string{"creaking_heart_state": "uprooted"})
	servertest.ExpectBlockNamed(t, srv, 11, 65, 3, "minecraft:creaking_heart",
		map[string]string{"creaking_heart_state": "uprooted"})
}

// TestACreakingHeartTakesRootAgainOnceItsLogsComeBack builds the columns the
// normal order — an uprooted heart placed first, its logs added after — one
// per axis: each log's placement books the heart's block tick, and the tick
// roots it once both logs hold. A log broken and then replaced takes the
// heart out through the ticker and back through the block tick.
// vanilla: CreakingHeartBlock.updateShape, CreakingHeartBlock.tick
func TestACreakingHeartTakesRootAgainOnceItsLogsComeBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.SetDayTime(6000)

	// Uprooted hearts with their logs still missing, one per axis.
	servertest.Setblock(t, c, 6, 65, 6, "minecraft:creaking_heart[axis=y,creaking_heart_state=uprooted]")
	servertest.Setblock(t, c, 10, 65, 6, "minecraft:creaking_heart[axis=x,creaking_heart_state=uprooted]")
	servertest.Setblock(t, c, 14, 65, 6, "minecraft:creaking_heart[axis=z,creaking_heart_state=uprooted]")

	// The logs the builder adds after the heart, one end at a time.
	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")
	servertest.Setblock(t, c, 9, 65, 6, "pale_oak_log[axis=x]")
	servertest.Setblock(t, c, 11, 65, 6, "pale_oak_log[axis=x]")
	servertest.Setblock(t, c, 14, 65, 5, "pale_oak_log[axis=z]")
	servertest.Setblock(t, c, 14, 65, 7, "pale_oak_log[axis=z]")

	for _, p := range [][3]int32{{6, 65, 6}, {10, 65, 6}, {14, 65, 6}} {
		x, y, z := p[0], p[1], p[2]
		servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, x, y, z) == "dormant" },
			"the uprooted heart never took root once both its logs were back")
		servertest.ExpectBlockNamed(t, srv, x, y, z, "minecraft:creaking_heart",
			map[string]string{"creaking_heart_state": "dormant"})
	}

	// A log broken and replaced takes the upright heart out again and back.
	servertest.Setblock(t, c, 6, 66, 6, "air")
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "uprooted" },
		"the heart never uprooted once its log was taken away")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "dormant" },
		"the heart never took root again once its log was replaced")
}
