package sleep

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

func TestABedClickFromAMinecartUnseatsTheSleeper(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)
	servertest.HoldTicks(t, srv)
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	cart := srv.SpawnMinecart(6.5, 64, 0.5)
	c.UseOn(cart)
	if riders := servertest.AwaitPassengersFor(c, cart); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("cart passengers = %v, want Actor", riders)
	}
	c.Settle()
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	assertBedClickUnseats(t, srv, c, cart)
	if riders := srv.MinecartPassengerIDs(cart); len(riders) != 0 {
		t.Errorf("sleep left cart passengers %v, want none", riders)
	}
}

func TestABedClickFromOtherMountsUnseatsOnlyWhenSleeping(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "pig", "cushion"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync, servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			servertest.StandSleepBed(t, srv)
			srv.SetPaused(true)
			srv.SetDayTime(testNoon)
			c.MoveTo(6.5, 64, 0.5, 0, 0)
			c.Settle()
			c.ChatCommand("summon minecraft:" + kind + " 6.5 64 0.5 {NoAI:1b,NoGravity:1b}")
			_, mount := c.AwaitNextSpawn()
			c.ChatCommand("ride @s mount @e[type=" + kind + ",limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			if riders := servertest.AwaitPassengersFor(c, mount.EntityID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
				t.Fatalf("mount passengers = %v, want Actor", riders)
			}
			c.Settle()
			c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
			c.ExpectSystemChat("block.minecraft.set_spawn", false)
			c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySetPassengers))
			if srv.Sleeping("Actor") {
				t.Fatal("daytime bed click put Actor to sleep")
			}

			servertest.SetNight(t, srv)
			c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
			assertBedClickUnseats(t, srv, c, mount.EntityID)
			// A vehicle tick must not move the sleeper back to its seat.
			servertest.RunTicks(t, srv, 1)
			if pos := srv.PlayerPosition("Actor"); pos.X != 5.5 || pos.Y != 64.6875 || pos.Z != 1.5 {
				t.Errorf("vehicle tick moved sleeping body to %+v", pos)
			}
		})
	}
}

func assertBedClickUnseats(t *testing.T, srv *server.Server, c *servertest.Client, vehicleID int32) {
	t.Helper()
	unseated := false
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetPassengers:
			vehicle, riders := servertest.ReadPassengers(c, data)
			if vehicle == vehicleID {
				if len(riders) != 0 {
					t.Errorf("sleep sent passengers %v, want none", riders)
				}
				unseated = len(riders) == 0
			}
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() != c.PlayerEntityID {
				return false
			}
			m := servertest.ReadSleepMetadata(t, data)
			if m.Sleeping {
				if !unseated {
					t.Error("sleep metadata arrived before the empty passenger list")
				}
				if m.Pose != protocol.PoseSleeping {
					t.Errorf("sleep pose = %d, want sleeping", m.Pose)
				}
				return true
			}
		case protocol.IDPlaySyncPosition:
			t.Error("sleep sent a dismount position instead of using the bed position")
		}
		return false
	}) {
		t.Fatal("bed click did not send sleeping metadata")
	}
	if !srv.Sleeping("Actor") {
		t.Error("bed click did not put Actor to sleep")
	}
	if pos := srv.PlayerPosition("Actor"); pos.X != 5.5 || pos.Y != 64.6875 || pos.Z != 1.5 {
		t.Errorf("sleeping position = %+v, want (5.5,64.6875,1.5)", pos)
	}
}

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

// TestADyingMonsterByTheBedKeepsTheSleeperUp kills the zombie and clicks the
// bed within its death animation: refused; after the animation it sleeps.
func TestADyingMonsterByTheBedKeepsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync, servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	c.ChatCommand("gamemode survival")
	servertest.AwaitSystemChatSaying(t, c, "commands.gamemode.success.self")
	c.ChatCommand("summon minecraft:zombie 7.5 64 2.5 {NoAI:1b}")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("kill @e[type=zombie]")
	servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")
	servertest.RunTicks(t, srv, 2)
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.not_safe", true)
	if srv.Sleeping("Actor") {
		t.Fatal("a dying monster by the bed did not keep the sleeper up")
	}

	servertest.RunTicks(t, srv, 25)
	c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	c.ExpectNextSleepMetadata()
	if !srv.Sleeping("Actor") {
		t.Error("the sleeper stayed up after the death animation")
	}
}

// TestCreativeSleepsBesideAMonster plants the same zombie by the bed of a
// creative player, whose sleep the monster check does not ask about.
func TestCreativeSleepsBesideAMonster(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	c.Login("Actor")
	c.Configure()
	// The zombie is planted before the body it guards against is in the
	// world: a spawn shown to nobody queues nothing, and the sleep's own
	// stream stays the click's alone.
	srv.SpawnMob("minecraft:zombie", 7.5, 64, 2.5)
	c.EnterWorld("Actor")

	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	if !srv.Sleeping("Actor") {
		t.Fatal("the creative did not sleep beside the zombie")
	}
}

// TestAnOccupiedBedAnswersTheClick has a second player click the bed a
// sleeper already lies in: the vanilla occupied message, and no second sleep.
func TestAnOccupiedBedAnswersTheClick(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	a := connect().Join("Actor")
	b := connect().Join("Watcher")

	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	if !srv.Sleeping("Actor") {
		t.Fatal("the first sleeper did not sleep")
	}

	b.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	// The first sleeper's sleep told both of them how many it still wants;
	// the click on the occupied bed answers after.
	b.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "1"}, protocol.Text{Text: "2"})
	b.ExpectSystemChat("block.minecraft.bed.occupied", true)
	if srv.Sleeping("Watcher") {
		t.Error("the second player slept in an occupied bed")
	}
	if !srv.Sleeping("Actor") {
		t.Error("the click unsettled the sleeper already in the bed")
	}
}

// TestTwoClicksRacingOneBedLeaveOneSleeper stands two clients' clicks
// against one free bed, round after round: both clicks are in the air at
// once, and the one whose claim wins sleeps. The round must leave exactly
// one sleeper in the bed and the bed saying so — never two bodies in one
// bed, never a bed taken with nobody in it.
func TestTwoClicksRacingOneBedLeaveOneSleeper(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	a := connect().Join("Actor")
	b := connect().Join("Watcher")

	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	a.MoveTo(6.5, 64, 0.5, 0, 0)
	a.Settle()
	b.MoveTo(7.5, 64, 0.5, 0, 0)
	b.Settle()

	for round := range bedRaceRounds {
		// The two clicks in the air together; each client's settle says
		// its own click has run to its end, so the race is settled
		// before the round is judged.
		clicked := make(chan struct{}, 2)
		for _, cc := range []*servertest.Client{a, b} {
			go func() {
				cc.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
				clicked <- struct{}{}
			}()
		}
		<-clicked
		<-clicked
		a.Settle()
		b.Settle()

		lyingA, lyingB := srv.Sleeping("Actor"), srv.Sleeping("Watcher")
		if lyingA == lyingB {
			t.Fatalf("round %d left the two clickers lying %v and %v", round, lyingA, lyingB)
		}
		servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
			"minecraft:red_bed", map[string]string{"occupied": "true"})

		// The sleeper stands back up and gives the bed to the next round.
		winner := b
		if lyingA {
			winner = a
		}
		winner.LeaveBed()
		winner.Settle()
		_, props := servertest.StateProps(srv.BlockAt(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ))
		if srv.Sleeping("Actor") || srv.Sleeping("Watcher") || props["occupied"] != "false" {
			t.Fatal("the sleeper to give the bed back")
		}
	}
}

// TestTheSleepersCountIsToldToEveryone has two players sleep one after the
// other and checks the message each sleep's recount makes to both of them:
// who is still wanted while one lies, and that the night will pass when the
// second lies down.
func TestTheSleepersCountIsToldToEveryone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	a := connect().Join("Actor")
	b := connect().Join("Watcher")

	// A second bed for the second sleeper, its head at (9,64,1).
	if !srv.SetBlock(9, 64, 1, servertest.BedStateName("red", "head", "south")) ||
		!srv.SetBlock(9, 64, 0, servertest.BedStateName("red", "foot", "south")) {
		t.Fatal("could not stand the second bed")
	}

	// The sleep's own news runs the message first, the order the vanilla
	// client sees: the count told, then the bed taken. The watcher's own
	// join message rides ahead of it on the first sleeper's stream, and
	// is read past with a word about what it was.
	join := a.NextSystemChat()
	if !bytes.Contains(join, []byte("multiplayer.player.joined")) {
		t.Fatalf("the first chat on the sleeper's stream is %q, want the watcher's join", join)
	}
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	a.MoveTo(6.5, 64, 0.5, 0, 0)
	a.Settle()
	a.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
	a.ExpectSystemChat("block.minecraft.set_spawn", false)
	a.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "1"}, protocol.Text{Text: "2"})
	a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "true"})
	a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedFootZ,
		"minecraft:red_bed", map[string]string{"occupied": "true", "part": "foot"})
	b.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "1"}, protocol.Text{Text: "2"})
	a.ExpectSleepMetadata()
	// The use-item tail's echoes of the first sleeper's own click.
	a.ExpectBedEchoes(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)

	// The second sleeper: the sleep begins and the message turns to the
	// night that will pass — the watcher hears the passing's word, the
	// sleeper's own stream goes block, metadata.
	b.MoveTo(10.5, 64, 0.5, 0, 0)
	b.Settle()
	b.UseBlock(9, 64, 1)
	b.ExpectNextBlockUpdate(9, 64, 1, "minecraft:red_bed", map[string]string{"occupied": "true"})
	b.ExpectNextSleepMetadata()
	a.ExpectSystemChat("sleep.skipping_night", true)
	if !srv.Sleeping("Actor") || !srv.Sleeping("Watcher") {
		t.Error("the two sleepers are not both asleep")
	}
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

// TestTheWakingStandUpFollowsTheYaw checks the waking orientation the
// vanilla stand-up turns by the sleeper's look: a sleeper facing south wakes
// out the west side of a south-facing bed, one facing west wakes out the
// east side of it.
func TestTheWakingStandUpFollowsTheYaw(t *testing.T) {
	t.Parallel()
	for _, want := range []struct {
		yaw           float32
		x, z, wakeYaw float64
	}{
		{0, 4.5, 1.5, -90},
		{90, 6.5, 1.5, 90},
	} {
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

		srv.StopTick()
		c.ExpectSleepStarted(srv, 6.5, 0.5, want.yaw)

		c.LeaveBed()
		srv.StartTick()
		for {
			id, data := c.NextSleepPacket()
			if id == protocol.IDPlaySyncPosition {
				sync := servertest.ReadSyncPosition(t, data)
				if sync.X != want.x || sync.Z != want.z {
					t.Errorf("yaw %f woke at (%f,%f), want (%f,%f)", want.yaw, sync.X, sync.Z, want.x, want.z)
				}
				if float64(sync.Yaw) != want.wakeYaw {
					t.Errorf("yaw %f woke facing %f, want %f", want.yaw, sync.Yaw, want.wakeYaw)
				}
				break
			}
		}
		if spawn, _, ok := srv.SpawnPointOf("Actor"); !ok || spawn.X != sleepBedHeadX {
			t.Errorf("the sleep left the spawn at (%v,%v,%v), want the head", spawn.X, spawn.Y, spawn.Z)
		}
	}
}

// TestTheWakingStepsPastABlockedStandUpCell lays a block collision in the
// west cell the south-facing stand-up tries first and checks the wake takes
// the next one: vanilla's stand-up refuses a cell whose collision reaches the
// top of the cell, so glass, a top slab and a coral block block the stand-up
// the way a full cube does, and the sleeper wakes in the cell past them
// instead of inside.
func TestTheWakingStepsPastABlockedStandUpCell(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block uint16
	}{
		{"glass", servertest.MustState(t, "minecraft:glass", nil)},
		{"top stone slab", servertest.MustState(t, "minecraft:stone_slab", map[string]string{"type": "top"})},
		{"tube coral block", servertest.MustState(t, "minecraft:tube_coral_block", nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

			srv.StopTick()
			// The first stand-up cell for a south-facing bed and a yaw
			// looking south is the west cell (4,64,1), whose standing is
			// (4.5,64,1.5).
			if !srv.SetBlock(sleepBedHeadX-1, sleepBedHeadY, sleepBedHeadZ, tc.block) {
				t.Fatal("could not lay the stand-up blocker")
			}
			c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

			c.LeaveBed()
			srv.StartTick()
			c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
			c.ExpectSystemChat("sleep.players_sleeping", true,
				protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
			sync := c.ExpectNextSyncPosition()
			if sync.X != 4.5 || sync.Y != 64 || sync.Z != 0.5 {
				t.Errorf("a %s beside the bed woke at (%f,%f,%f), want the cell past it (4.5,64,0.5)",
					tc.name, sync.X, sync.Y, sync.Z)
			}
			if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Y != 64 || got.Z != 0.5 {
				t.Errorf("a %s beside the bed left the body at (%f,%f,%f), want (4.5,64,0.5)",
					tc.name, got.X, got.Y, got.Z)
			}
		})
	}
}

// TestTheWakingStepsPastAStandUpCellWithoutRoom checks the two other ways the
// first stand-up cell fails: a carpet for its floor, which rises less than a
// cell, and a sea pickle that lifts the 1.8-tall body into a glass block two
// cells up.
// vanilla: DismountHelper.findSafeDismountLocation, BlockGetter.getBlockFloorHeight
func TestTheWakingStepsPastAStandUpCellWithoutRoom(t *testing.T) {
	t.Parallel()
	carpet := servertest.MustState(t, "minecraft:white_carpet", nil)
	for _, tc := range []struct {
		name   string
		blocks [][2]any // y offset from the bed, state
	}{
		{"carpet floor", [][2]any{{int32(-1), carpet}}},
		{"sea pickle under a ceiling", [][2]any{
			{int32(0), servertest.MustState(t, "minecraft:sea_pickle", nil)},
			{int32(2), servertest.MustState(t, "minecraft:glass", nil)},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

			srv.StopTick()
			for _, b := range tc.blocks {
				if !srv.SetBlock(sleepBedHeadX-1, sleepBedHeadY+b[0].(int32), sleepBedHeadZ, b[1].(uint16)) {
					t.Fatal("could not lay the block")
				}
			}
			c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

			c.LeaveBed()
			srv.StartTick()
			c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
			c.ExpectSystemChat("sleep.players_sleeping", true,
				protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
			sync := c.ExpectNextSyncPosition()
			if sync.X != 4.5 || sync.Y != 64 || sync.Z != 0.5 {
				t.Errorf("%s woke at (%f,%f,%f), want the cell past it (4.5,64,0.5)", tc.name, sync.X, sync.Y, sync.Z)
			}
		})
	}
}

// TestTheWakingStandsOnACarpetInTheStandUpCell lays a carpet in the first
// stand-up cell: its floor height is the carpet's 1/16, under 1, so the waker
// stands on it instead of stepping past.
// vanilla: DismountHelper.findSafeDismountLocation
func TestTheWakingStandsOnACarpetInTheStandUpCell(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	carpet := servertest.MustState(t, "minecraft:white_carpet", nil)
	if !srv.SetBlock(sleepBedHeadX-1, sleepBedHeadY, sleepBedHeadZ, carpet) {
		t.Fatal("could not lay the carpet")
	}
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	c.LeaveBed()
	srv.StartTick()
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	sync := c.ExpectNextSyncPosition()
	if sync.X != 4.5 || sync.Y != 64.0625 || sync.Z != 1.5 {
		t.Errorf("a carpet beside the bed woke at (%f,%f,%f), want on it (4.5,64.0625,1.5)", sync.X, sync.Y, sync.Z)
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

// TestAdvanceTimeOffKeepsTheClockThroughTheSleepSkip drives a hundred and one
// turns of the sleeping pass with the advance_time rule off and checks what
// the night does with the clock held still: the sleeper is still woken and
// the rain is still reset, but the clock stays at the midnight it lay down
// at — the wake-up marker rides the same rule that advances the clocks.
func TestAdvanceTimeOffKeepsTheClockThroughTheSleepSkip(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.SetGameRule("advance_time", false)
	srv.SetWeatherState(true, false, 1000, 1000, 0)
	// The skip resets the weather only once the rain level is past the 0.2
	// Level.isRaining tests, so the fade is driven up first.
	for range 21 {
		srv.TickWeather()
	}

	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}
	if _, dayTime := srv.Time(); dayTime != testMidnight {
		t.Errorf("advance_time off left the clock at %d, want the midnight %d", dayTime, testMidnight)
	}
	if srv.Sleeping("Actor") {
		t.Error("the night held the clock still and left the sleeper lying")
	}
	if srv.Raining() {
		t.Error("the night held the clock still and left the rain falling")
	}

	// The stream says the same: no Update Time for a jump that never ran,
	// the wake's own news first.
	srv.StartTick()
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

// TestTheSleepingPercentageRuleAboveTheWholeHoldsTheNight sets the share above
// the whole — one no player can meet — and checks the sleeper is told the
// night cannot pass and that a hundred ticks of lying leave the clock where it
// was.
func TestTheSleepingPercentageRuleAboveTheWholeHoldsTheNight(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	a := connect().Join("Actor")
	connect().Join("Watcher")

	srv.SetGameRuleValue("players_sleeping_percentage", 101)
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	a.MoveTo(6.5, 64, 0.5, 0, 0)
	a.Settle()
	a.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)

	// The spawn the click set, then the overlay a share above the whole
	// tells the sleeper as they lie down.
	a.ExpectSystemChat("block.minecraft.set_spawn", false)
	a.ExpectSystemChat("sleep.not_possible", true)
	a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "true"})
	a.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedFootZ,
		"minecraft:red_bed", map[string]string{"occupied": "true", "part": "foot"})
	a.ExpectSleepMetadata()
	a.ExpectUseEchoes(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ, protocol.FaceTop)

	if !srv.Sleeping("Actor") {
		t.Fatal("the click did not put the player to sleep")
	}
	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}
	if _, dayTime := srv.Time(); dayTime != testMidnight {
		t.Errorf("the rule above the whole left the clock at %d, want the midnight %d", dayTime, testMidnight)
	}
	if !srv.Sleeping("Actor") {
		t.Error("the rule above the whole stood the sleeper up anyway")
	}
}

// TestSleepSkipInTheFirstTicksKeepsTheRain drives a sleep skip while the rain
// is still fading in: /weather rain sets the flag, the level under 0.2 makes
// Level.isRaining false, and vanilla's ServerLevel.tick resets the weather
// only when isRaining is true, so the storm stays up.
func TestSleepSkipInTheFirstTicksKeepsTheRain(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 19 {
		srv.TickWeather()
	}
	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}

	if !srv.Raining() {
		t.Error("the skip within twenty ticks stopped the rain")
	}
	if srv.Sleeping("Actor") {
		t.Error("the night did not pass")
	}
}

// TestTheDawnStandsTheSleeperUp sleeps a night away into a day and checks
// the wake the day's light makes: the sleeper standing, the message the
// recount makes and the position the bed handed out.
func TestTheDawnStandsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	srv.SetDayTime(testNoon)
	srv.TickSleeping()

	if srv.Sleeping("Actor") {
		t.Fatal("the dawn left the sleeper lying")
	}
	srv.StartTick()
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	c.ExpectNextSyncPosition()
	c.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "false"})
	if m := c.ExpectNextSleepMetadata(); m.Sleeping {
		t.Error("the dawn left the metadata asleep")
	}
}

// TestAWakeRacingTheSleepsBeginningLeavesTheBedFree stands the tick's dawn
// wake against a sleep's beginning, round after round: a goroutine watches
// for the body to lie down and fires the dawn the moment it does, while the
// click's beginning is still in the air. Whatever the race wins, what the
// round leaves behind must agree with itself — the bed's occupancy saying
// the same as the body's sleep — never a bed left occupied with nobody in it.
func TestAWakeRacingTheSleepsBeginningLeavesTheBedFree(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()

	for round := range sleepRaceRounds {
		// The dawn the tick's pass answers, fired the moment the body
		// lies — the step a wake claims a sleep at, inside the beginning
		// whenever the beginning has not finished claiming.
		fired := make(chan bool, 1)
		go func() {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if srv.Sleeping("Actor") {
					srv.SetDayTime(testNoon)
					srv.TickSleeping()
					fired <- true
					return
				}
				// A yield, so the click's own goroutine gets a core on
				// the two the race runs on.
				runtime.Gosched()
			}
			fired <- false
		}()
		c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
		if !<-fired {
			t.Fatal("the sleep the click began never began")
		}
		// The dawn left the clock at noon, where no bed takes a sleeper;
		// the next round's click wants the night back.
		srv.SetDayTime(testMidnight)
		// The settle tells the click's whole beginning has run, so what
		// the round left behind is what these reads see.
		c.Settle()

		lying := srv.Sleeping("Actor")
		_, props := servertest.StateProps(srv.BlockAt(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ))
		if occupied := props["occupied"] == "true"; occupied != lying {
			t.Fatalf("round %d left the body lying %v and the bed occupied %v", round, lying, occupied)
		}
	}

	if srv.Sleeping("Actor") {
		t.Fatal("the races left the sleeper lying")
	}
	if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Y != 64 || got.Z != 1.5 {
		t.Errorf("woke body stands at (%f,%f,%f), want (4.5,64,1.5)", got.X, got.Y, got.Z)
	}
	servertest.ExpectBlockNamed(t, srv, sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "false"})
}

// TestBedClicksSpammedAtAWakeKeepThePoseTheServerKeeps stands a wake by a
// blow against the sleeper's own client spamming the bed, round after
// round. Whatever a round leaves behind — the sleep ended, or a new one
// the spam began — the last pose the client was told and the bed's
// occupancy must say what the server's own sleep state says.
func TestBedClicksSpammedAtAWakeKeepThePoseTheServerKeeps(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()

	for round := range wakeRaceRounds {
		// A landed blow costs five hearts and leaves the body
		// invulnerable for ten ticks; the next round's blow wants both
		// back.
		srv.SetHealth("Actor", servertest.MaxHealthTestBase)
		c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
		c.Settle()
		if !srv.Sleeping("Actor") {
			t.Fatal("the click to put the body down")
		}

		// The wake and the spam of clicks together, the clicks spread so
		// some of them land in the wake's flight.
		woke := make(chan bool, 1)
		go func() { woke <- srv.Hurt("Actor", 5) }()
		for range bedClickSpam {
			c.UseBlock(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ)
			time.Sleep(time.Millisecond)
		}
		if !<-woke {
			t.Fatal("the wake's blow never landed")
		}

		poses := c.SleepPosesBeforeSettle(c.PlayerEntityID)
		lying := srv.Sleeping("Actor")
		want := int32(protocol.PoseStanding)
		if lying {
			want = int32(protocol.PoseSleeping)
		}
		if len(poses) == 0 || poses[len(poses)-1] != want {
			t.Fatalf("round %d left the last pose %v with the server holding lying=%v", round, poses, lying)
		}
		_, props := servertest.StateProps(srv.BlockAt(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ))
		if occupied := props["occupied"] == "true"; occupied != lying {
			t.Fatalf("round %d left the body lying %v and the bed occupied %v", round, lying, occupied)
		}

		// A new sleep the spam began stands back up for the next round,
		// the dawn the tick's pass would run.
		if lying {
			srv.SetDayTime(testNoon)
			srv.TickSleeping()
			srv.SetDayTime(testMidnight)
			_, props := servertest.StateProps(srv.BlockAt(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ))
			if srv.Sleeping("Actor") || props["occupied"] != "false" {
				t.Fatal("the dawn to stand the body up")
			}
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

// TestABlowStandsTheSleeperUp hurts a sleeping player and checks the wake
// the blow makes, the sleep ended for good with nothing said about it by
// the hurt itself.
func TestABlowStandsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	if !srv.Hurt("Actor", 5) {
		t.Fatal("the blow did not land")
	}
	srv.StartTick()
	if srv.Sleeping("Actor") {
		t.Fatal("the blow left the sleeper lying")
	}
	c.ExpectNextAnimation(c.PlayerEntityID, protocol.AnimationLeaveBed)
	c.ExpectSystemChat("sleep.players_sleeping", true,
		protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	c.ExpectNextSyncPosition()
	c.ExpectNextBlockUpdate(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "false"})
	c.ExpectNextSleepMetadata()
}

// TestALeavingSleeperGivesTheBedBack has a sleeper leave and checks the bed
// they give back: the head unoccupied, the way the vanilla disconnect's own
// wake leaves it.
func TestALeavingSleeperGivesTheBedBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	c.Disconnect()
	// The departure can unload the chunk, which BlockAt reads as air, so
	// the bed is read back through a load of its chunk.
	name, props := servertest.StateProps(srv.LoadBlockAt(sleepBedHeadX, sleepBedHeadY, sleepBedHeadZ))
	if name != "minecraft:red_bed" || props["occupied"] != "false" {
		t.Errorf("the bed head is %s %v after the sleeper left, want an unoccupied minecraft:red_bed", name, props)
	}
}

// TestMovingWhileAsleepLeavesTheBodyInTheBed sends a sleeper's client
// somewhere else and checks the server leaves the body where the bed put
// it, the movement the vanilla server ignores a sleeper for, and puts the
// body that got away by more than a block back.
func TestMovingWhileAsleepLeavesTheBodyInTheBed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	c.MoveTo(20.5, 64, 20.5, 0, 0)
	srv.StartTick()
	if got := srv.PlayerPosition("Actor"); got.X != 5.5 || got.Y != 64.6875 || got.Z != 1.5 {
		t.Fatalf("a sleeping body was moved to (%f,%f,%f)", got.X, got.Y, got.Z)
	}
	sync := c.ExpectNextSyncPosition()
	if sync.X != 5.5 || sync.Y != 64.6875 || sync.Z != 1.5 {
		t.Errorf("the resync put the body at (%f,%f,%f)", sync.X, sync.Y, sync.Z)
	}
}

// TestAMoveSentBeforeTheWakeLeavesTheStandUp wakes a sleeper by hand and has
// their client report a step it sent before it saw the wake: the server
// keeps the stand-up spot until the client accepts the wake's teleport, and
// takes the client's moves again after.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer, awaitingPositionFromClient
func TestAMoveSentBeforeTheWakeLeavesTheStandUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.SetDayTime(testNoon)
	srv.TickSleeping()
	if srv.Sleeping("Actor") {
		t.Fatal("the dawn left the sleeper lying")
	}

	// Sent ahead of the accept, which the harness sends as it reads the
	// wake's position.
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Y != 64 || got.Z != 1.5 {
		t.Errorf("a move sent before the wake was seen put the body at (%f,%f,%f), want (4.5,64,1.5)",
			got.X, got.Y, got.Z)
	}

	c.MoveTo(4.5, 64, 2.5, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition("Actor"); got.X != 4.5 || got.Z != 2.5 {
		t.Errorf("a move after the accept left the body at (%f,%f,%f), want (4.5,64,2.5)",
			got.X, got.Y, got.Z)
	}
}

// TestCreativeEatsWithAFullBarAndKeepsTheItem feeds a creative player whose
// bar is full and checks the meal both begins and ends without taking the
// item out of the hand, the vanilla infinite materials at work. The main
// loop is stopped so the meal's ticks are the test's to spend.
func TestCreativeEatsWithAFullBarAndKeepsTheItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	c.Join("Actor")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:bread", 3))
	srv.SetFood("Actor", 20)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if !srv.Eating("Actor") {
		t.Fatal("the creative with a full bar begins a meal")
	}
	for srv.Eating("Actor") {
		srv.TickSurvival()
	}
	if got := srv.InventorySlot("Actor", int(player.HotbarSlot)); got.Count != 3 {
		t.Errorf("the creative's bread ran to %d, want the 3 it began with", got.Count)
	}
}

// sleepTicksToSkip mirrors the server's own constant, the hundred ticks a
// sleeper has to lie for before the night passes for them.
const sleepTicksToSkip = 100

// TestAPhantomByTheBedLetsTheSleeperSleep plants a phantom beside the bed: it
// is hostile but no Monster, the class the check searches.
// vanilla: ServerPlayer.startSleepInBed
func TestAPhantomByTheBedLetsTheSleeperSleep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.SurvivalConfig(), servertest.NoTimeSync)
	srv.StopTick()
	c.Login("Actor")
	c.Configure()
	srv.SpawnMob("minecraft:phantom", 7.5, 65, 2.5)
	c.EnterWorld("Actor")

	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	if !srv.Sleeping("Actor") {
		t.Error("a phantom by the bed kept the sleeper up")
	}
}

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

// TestACalmNeutralOrNonMonsterByTheBedLetsTheSleeperSleep plants mobs that are
// Enemy but no Monster (or a calm zombified piglin) beside the bed.
// vanilla: ServerPlayer.startSleepInBed, ZombifiedPiglin.isPreventingPlayerRest
func TestACalmNeutralOrNonMonsterByTheBedLetsTheSleeperSleep(t *testing.T) {
	t.Parallel()
	for _, mob := range []string{"minecraft:zombified_piglin", "minecraft:hoglin", "minecraft:shulker", "minecraft:ghast"} {
		t.Run(mob, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, sleepMobConfig(), servertest.NoTimeSync)
			servertest.FreezeUntilHeld(t, srv)
			c := connect().Join("Actor")
			servertest.HoldTicks(t, srv)
			srv.SpawnMob(mob, 7.5, 64, 2.5)

			clickBedAtNight(t, srv, c)

			if !srv.Sleeping("Actor") {
				t.Errorf("a %s by the bed kept the sleeper up", mob)
			}
		})
	}
}

// TestAnAngryZombifiedPiglinByTheBedKeepsTheSleeperUp: under universal anger a
// piglin with anger time left is angry at every valid player.
// vanilla: ZombifiedPiglin.isPreventingPlayerRest, NeutralMob.isAngryAt
func TestAnAngryZombifiedPiglinByTheBedKeepsTheSleeperUp(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, sleepMobConfig(), servertest.NoTimeSync)
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Actor")
	servertest.HoldTicks(t, srv)
	srv.SetGameRule("universal_anger", true)
	id := srv.SpawnMob("minecraft:zombified_piglin", 7.5, 64, 2.5)
	if !srv.AngerMob(id, 600) {
		t.Fatal("no piglin to anger")
	}

	clickBedAtNight(t, srv, c)

	if srv.Sleeping("Actor") {
		t.Error("an angry zombified piglin by the bed let the sleeper sleep")
	}
}
