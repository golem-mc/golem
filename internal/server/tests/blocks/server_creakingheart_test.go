package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestACreakingHeartPlacedBetweenItsLogsStartsByTheHour places one from the
// hand onto the top face of the log below it: the uprooted default a heart
// without its logs keeps turns awake or dormant at the placement, by the hour
// the world is at.
// vanilla: CreakingHeartBlock.getStateForPlacement
func TestACreakingHeartPlacedBetweenItsLogsStartsByTheHour(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")

	// By day a heart placed between the two logs starts dormant.
	servertest.PlaceFrom(c, "minecraft:creaking_heart", 4, 6, 0, 6, 64, 6, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 6, "minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "dormant"})

	// At midnight the same placement starts awake. The cell is cleared
	// between the two; the placement's own break drops nothing in creative.
	servertest.Setblock(t, c, 6, 65, 6, "air")
	srv.SetDayTime(18000)
	servertest.PlaceFrom(c, "minecraft:creaking_heart", 4, 6, 0, 6, 64, 6, protocol.FaceTop)
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

// TestACreakingHeartLeavesTheHourSwitchAndUprootingToTheTicker puts the next
// neighbour change's block tick on a heart whose state is not uprooted: the
// ticker is what switches a heart by the hour and uproots it, so a stone
// placed beside a dormant heart at midnight and a log taken off an awake one
// both wait for the ticker, not the next game tick's block tick.
// vanilla: CreakingHeartBlock.tick, updateState, CreakingHeartBlockEntity.updateCreakingState
func TestACreakingHeartLeavesTheHourSwitchAndUprootingToTheTicker(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.SetDayTime(6000)

	// An awake heart between its logs, taken dormant by the ticker by day;
	// the transition leaves the ticker just reset, ~20 ticks from firing.
	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 65, 6, "minecraft:creaking_heart[axis=y,creaking_heart_state=awake]")
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "dormant" },
		"the awake heart never went dormant by day")

	// Midnight and a stone beside the heart: the block tick the neighbour
	// change books leaves the hour switch to the ticker.
	srv.SetDayTime(18000)
	servertest.Setblock(t, c, 5, 65, 6, "stone")
	servertest.AwaitTicks(t, srv, 2)
	if got := creakingHeartStateAt(srv, 6, 65, 6); got != "dormant" {
		t.Fatalf("the heart = %s three ticks after a neighbour change at midnight, want dormant until the ticker", got)
	}

	// The ticker wakes it, resetting its countdown again; taking the top
	// log then leaves the uprooting to the ticker too.
	servertest.WaitForTicks(t, srv, 40, func() bool { return creakingHeartStateAt(srv, 6, 65, 6) == "awake" },
		"the dormant heart never woke at midnight")
	servertest.Setblock(t, c, 6, 66, 6, "air")
	servertest.AwaitTicks(t, srv, 2)
	if got := creakingHeartStateAt(srv, 6, 65, 6); got != "awake" {
		t.Fatalf("the heart = %s three ticks after its log was removed, want awake until the ticker", got)
	}
}

// TestACreakingHeartItemThatNamesUprootedStaysUprooted places a heart whose
// block_state component names uprooted between two upright logs: the
// component applies after the placement's own hour read, so the item's
// uprooted wins, the vanilla BlockItem order.
// vanilla: CreakingHeartBlock.getStateForPlacement, BlockItem.updateBlockStateFromTag
func TestACreakingHeartItemThatNamesUprootedStaysUprooted(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.SetDayTime(6000)

	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemWithBlockState(t, "minecraft:creaking_heart",
		[2]string{"axis", "y"}, [2]string{"creaking_heart_state", "uprooted"}))
	c.MoveToGround(4.5, 64, 6.5, true)
	c.FacePlayer(4.5, 64, 6.5, 0, 0)
	c.Settle()
	c.PlaceBlock(6, 64, 6, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 65, 6, "minecraft:creaking_heart",
		map[string]string{"creaking_heart_state": "uprooted"})
}

// TestAnObserverWatchingACreakingHeartPulsesOnItsStateChange checks that
// the heart's own state change is a write its neighbours hear: an observer
// facing the heart sees the flip and pulses, lighting the lamp behind its
// back.
// vanilla: CreakingHeartBlockEntity.serverTick, Level.setBlock flag 3
func TestAnObserverWatchingACreakingHeartPulsesOnItsStateChange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// A heart column awake through the midnight stretch, an observer
	// watching its middle from beside it, and a lamp behind the observer.
	srv.SetDayTime(18000)
	servertest.Setblock(t, c, 6, 64, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 66, 6, "pale_oak_log")
	servertest.Setblock(t, c, 6, 65, 6, "minecraft:creaking_heart[axis=y,creaking_heart_state=awake]")
	servertest.Setblock(t, c, 7, 65, 6, "observer[facing=west]")
	servertest.Setblock(t, c, 8, 65, 6, "redstone_lamp")
	servertest.AwaitTicks(t, srv, 40)
	if got := creakingHeartStateAt(srv, 6, 65, 6); got != "awake" {
		t.Fatalf("setup: the heart is %s at midnight, want awake", got)
	}

	// The flip to dormant is one the observer sees: it pulses for two game
	// ticks and lights the lamp behind its output.
	srv.SetDayTime(6000)
	servertest.WaitForTicks(t, srv, 80, func() bool { return servertest.LampLitAt(srv, 8, 65, 6) },
		"the lamp behind the observer never lit when the heart went dormant")
}

// TestACreakingHeartInTheNetherAndTheEndStaysDormant stands one awake
// between its logs in each of the other dimensions: their timelines carry
// no creaking-active track, so the midnight the overworld's clock tells its
// own hearts to wake never reaches them, and both settle dormant.
// vanilla: EnvironmentAttributes.CREAKING_ACTIVE, timeline tags in_nether and in_end
func TestACreakingHeartInTheNetherAndTheEndStaysDormant(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())

	logState, ok := registry.FindState("minecraft:pale_oak_log", nil)
	if !ok {
		t.Fatal("the registry has no pale oak log")
	}
	awake := creakingHeartState(t, "awake")
	for _, dim := range []string{world.NetherName, world.EndName} {
		release := srv.HoldChunkInDimension(dim, 6, 6)
		defer release()
		// No player is in this dimension, so only a forced chunk ticks its block entities.
		srv.ForceChunkInDimension(dim, 0, 0)
		srv.SetBlockInDimension(dim, 6, 64, 6, logState)
		srv.SetBlockInDimension(dim, 6, 66, 6, logState)
		srv.SetBlockInDimension(dim, 6, 65, 6, awake)
		if !srv.EnsureBlockEntityInDimension(dim, 6, 65, 6, world.KindCreakingHeart) {
			t.Fatalf("the %s heart keeps no block entity", dim)
		}
	}

	srv.SetDayTime(18000)
	servertest.AwaitTicks(t, srv, 40)
	for _, dim := range []string{world.NetherName, world.EndName} {
		name, props := servertest.StateProps(srv.BlockInDimension(dim, 6, 65, 6))
		if name != "minecraft:creaking_heart" || props["creaking_heart_state"] != "dormant" {
			t.Errorf("the awake heart in %s = %s[%v], want dormant", dim, name, props)
		}
	}
}
