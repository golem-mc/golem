package sleep

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The sleeping tests: the bed click that puts a body down for the night, the
// night the sleeper's hundred ticks pass, and the wakes — the sleeper's own
// leave bed, the dawn, a bed gone under its sleeper, a blow and a leaving.
// The tests that click read the sleep's own packets straight off the stream,
// the ones that tick a sleeper drive the sleeping pass by hand with the main
// loop stopped, the way the loading tests run their grace out.

// midnight and noon are the times of day the sleep tests set the paused
// clock to: midnight is night by the sky-darkening rule the bed's can_sleep
// reads, noon is day by it. The clock is paused wherever one is set, so a
// main loop tick cannot move the hour between a click and what the test
// checks.
const (
	testMidnight = 18000
	testNoon     = 1000
)

// sleepRaceRounds is how many rounds the sleep's races run: the interleaving
// a race needs is a rare one, so a round that does not catch it is cheap and
// many of them are wanted.
const sleepRaceRounds = 60

// bedRaceRounds is the same budget for the race of two clicks on one bed.
const bedRaceRounds = 60

// wakeRaceRounds is the same budget for the race of a wake and bed clicks,
// whose rounds cost the metadata the judgment reads a little longer to
// gather.
const wakeRaceRounds = 40

// bedClickSpam is how many clicks a wake race's round spams, a
// millisecond apart, to give some of them the wake's flight to land in.
const bedClickSpam = 10

// sleepBedHead is where the tests stand the bed's head, facing south, its
// foot at (5,64,0): the arrangement every sleep test shares, with the
// sleeper standing beside it or the yaw the stand-up test wants.
const (
	sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ = 5, 64, 1
	sleepBedFootZ                               = sleepBedHeadZ - 1
)

// TestBedClickAtNightPutsTheBodyInTheBed sleeps a night click and checks
// what it leaves behind: the sleeper asleep, the head occupied and the foot
// not, the body's position the bed's centre, the spawn point the head, and
// the metadata the sleeper's own client moves their body into the bed on.
func TestBedClickAtNightPutsTheBodyInTheBed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	if !srv.Sleeping("Actor") {
		t.Fatal("the click did not put the player to sleep")
	}
	if got := srv.PlayerPosition("Actor"); got.X != 5.5 || got.Y != 64.6875 || got.Z != 1.5 {
		t.Errorf("sleeping body stands at (%f,%f,%f), want (5.5,64.6875,1.5)", got.X, got.Y, got.Z)
	}
	// The foot's own occupied copy, the vanilla shape update the head's
	// flip tells the physics with.
	servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedFootZ, "minecraft:red_bed", map[string]string{"occupied": "true", "part": "foot"})
	if spawn, _, ok := srv.SpawnPointOf("Actor"); !ok || spawn.X != sleepBedHeadX || spawn.Y != sleepBedHeadY || spawn.Z != sleepBedHeadZ {
		t.Errorf("the sleep left the spawn at (%v,%v,%v), want the head", spawn.X, spawn.Y, spawn.Z)
	}
}

// TestBedClickByDayTellsTheSleeperTheSunIsUp clicks a bed at noon and checks
// the refusal: the no-sleep message, nobody asleep and no occupancy.
func TestBedClickByDayTellsTheSleeperTheSunIsUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	servertest.StandSleepBed(t, srv)
	srv.SetPaused(true)
	srv.SetDayTime(testNoon)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)

	// The spawn the click set even though the sleep is refused, then the
	// refusal itself.
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)
	if srv.Sleeping("Actor") {
		t.Error("a day click put the player to sleep")
	}
	servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, "minecraft:red_bed", map[string]string{"occupied": "false"})
}

// TestAMonsterByTheBedKeepsTheSleeperUp plants a zombie beside the bed and
// checks the night click is refused with the not-safe message. The main loop
// is stopped so the zombie's own mind cannot act between the spawn and the
// click.
func TestAMonsterByTheBedKeepsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.SurvivalConfig(), servertest.NoTimeSync)
	srv.StopTick()
	c.Join("Actor")

	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	srv.SpawnMob("minecraft:zombie", 7.5, 64, 2.5)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)

	// The spawn the click set even though the sleep is refused, then the
	// refusal itself.
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.not_safe", true)
	if srv.Sleeping("Actor") {
		t.Error("a monster by the bed did not keep the sleeper up")
	}
	servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, "minecraft:red_bed", map[string]string{"occupied": "false"})
}

// TestLeaveBedStandsTheSleeperUp sends the leave-bed command and checks the
// wake in the order it is sent: the animation, the bed unoccupied, the pose
// standing and the sleeping position emptied, the message the leaving's
// recount makes, and the position the bed handed out.
func TestLeaveBedStandsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	// The tick loop is stopped before the sleep starts, so the sleep's own
	// hundred ticks cannot run the night past the test's wake — a night
	// the loop passes would wake the sleeper before the leave bed arrives,
	// and on a slow machine those ticks can pass before the test gets to
	// stop the loop after the lying down.
	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	c.LeaveBed()
	srv.StartTick()
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	sync := c.ExpectNextSyncPosition()
	if sync.X != 4.5 || sync.Y != 64 || sync.Z != 1.5 {
		t.Errorf("wake position = (%f,%f,%f), want (4.5,64,1.5)", sync.X, sync.Y, sync.Z)
	}
	if sync.Yaw != -90 {
		t.Errorf("wake yaw = %f, want -90", sync.Yaw)
	}
	c.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "false"})
	m := c.ExpectNextSleepMetadata()
	if m.Pose != protocol.PoseStanding {
		t.Errorf("wake metadata pose = %d, want %d", m.Pose, protocol.PoseStanding)
	}
	if m.Sleeping {
		t.Error("wake metadata still names a bed")
	}
	if srv.Sleeping("Actor") {
		t.Error("the leave bed left the player asleep")
	}
	if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Y != 64 || got.Z != 1.5 {
		t.Errorf("woke body stands at (%f,%f,%f), want (4.5,64,1.5)", got.X, got.Y, got.Z)
	}
}

// TestTheNightPassesWhenTheSleeperHasLainAHundredTicks drives a hundred and
// one turns of the sleeping pass and checks the night the sleeper passes:
// the clock jumps to the wake marker, the rain the sleep was slept through
// stops, the body stands up and the time it jumped to is told.
func TestTheNightPassesWhenTheSleeperHasLainAHundredTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.SetWeatherState(true, false, 1000, 1000, 0)
	// The skip resets the weather only once the rain level is past the 0.2
	// Level.isRaining tests, so the fade is driven up first.
	for range 21 {
		srv.TickWeather()
	}

	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}
	_, dayTime := srv.Time()
	if dayTime != 24000 {
		t.Errorf("the night left the clock at %d, want 24000", dayTime)
	}
	if srv.Sleeping("Actor") {
		t.Error("the night passed and left the sleeper lying")
	}
	if srv.Raining() {
		t.Error("the night passed and left the rain falling")
	}
	if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Y != 64 || got.Z != 1.5 {
		t.Errorf("woke body stands at (%f,%f,%f), want (4.5,64,1.5)", got.X, got.Y, got.Z)
	}

	// The packets the hand-driven wake queued, read once the loop is
	// back, in the order the vanilla skip runs them: the time the jump
	// set, told before the waking runs; then the wake itself, its
	// animation, its position and its bed news, with no message about
	// it — the waking wakes are not ones the recount would tell.
	srv.StartTick()
	id, _ := c.NextSleepPacket()
	if id != protocol.IDPlayUpdateTime {
		t.Fatalf("the skip's first packet is 0x%02X, want the update time", id)
	}
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectNextSyncPosition()
	c.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "false"})
	if m := c.ExpectNextSleepMetadata(); m.Sleeping {
		t.Error("the night's passing left the metadata asleep")
	}
}

// TestTheSleepingPercentageRuleLetsOneSleeperPassTheNight sets the share the
// night needs below the whole and checks a lone sleeper of two passes it: the
// rule at 50 asks for one of the pair, and at 0 for the one a share of nothing
// still rounds up to.
func TestTheSleepingPercentageRuleLetsOneSleeperPassTheNight(t *testing.T) {
	t.Parallel()
	for _, pct := range []int32{0, 50} {
		srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
		srv.StopTick()
		a := connect().Join("Actor")
		connect().Join("Watcher")

		srv.SetGameRuleValue("players_sleeping_percentage", pct)
		servertest.StandSleepBed(t, srv)
		servertest.SetNight(t, srv)
		a.MoveTo(6.5, 64, 0.5, 0, 0)
		a.Settle()
		a.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)

		// The spawn the click set, then the recount's word: the one sleeper
		// already meets the share, so the night will pass.
		a.ExpectSystemChat("block.minecraft.set_spawn", false)
		a.ExpectSystemChat("sleep.skipping_night", true)
		a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
			"minecraft:red_bed", map[string]string{"occupied": "true"})
		a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedFootZ,
			"minecraft:red_bed", map[string]string{"occupied": "true", "part": "foot"})
		a.ExpectSleepMetadata()
		a.ExpectUseEchoes(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, protocol.FaceTop)

		for range sleepTicksToSkip + 1 {
			srv.TickSleeping()
		}
		if _, dayTime := srv.Time(); dayTime != 24000 {
			t.Fatalf("the rule at %d left the clock at %d after the night, want 24000", pct, dayTime)
		}
		if srv.Sleeping("Actor") {
			t.Fatalf("the rule at %d passed the night and left the sleeper lying", pct)
		}
	}
}

// TestBreakingTheBedStandsTheSleeperUp takes the bed out from under its
// sleeper and checks the wake: the body left where it lay, the bed the
// vanilla stopSleeping finds nothing of, and the foot untouched.
func TestBreakingTheBedStandsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	if !srv.SetBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, 0) {
		t.Fatal("could not break the bed")
	}
	srv.TickSleeping()

	if srv.Sleeping("Actor") {
		t.Fatal("a broken bed left the sleeper lying")
	}
	if got := srv.PlayerPosition("Actor"); got.X != 5.5 || got.Y != 64.6875 || got.Z != 1.5 {
		t.Errorf("the body left the bed it woke out of: stands at (%f,%f,%f)", got.X, got.Y, got.Z)
	}
	// The foot the head left behind: still a bed, stale in the occupied
	// copy the shape update gave it — the break went through the world
	// setter the tests use, which tells no physics, and the vanilla wake
	// of a gone bed unoccupies nothing.
	servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedFootZ, "minecraft:red_bed", nil)

	srv.StartTick()
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	// The waking re-sends the body's own position, the vanilla
	// stopSleepInBed teleport that runs whatever the bed the wake found
	// — a gone bed sends it with the body unchanged.
	c.ExpectNextSyncPosition()
	if m := c.ExpectNextSleepMetadata(); m.Sleeping {
		t.Error("the broken bed's wake left the metadata asleep")
	}
}

// sleepTicksToSkip mirrors the server's own constant, the hundred ticks a
// sleeper has to lie for before the night passes for them.
const sleepTicksToSkip = 100

// clickBedAtNight clicks the shared bed from beside it at night on held ticks
// and runs the ticks the click's effects need.
func clickBedAtNight(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
}

// sleepMobConfig is the survival config at the minimum distances: the bed and
// the mob are both in the spawn chunk.
func sleepMobConfig() *config.Config {
	cfg := servertest.SurvivalConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	return cfg
}
