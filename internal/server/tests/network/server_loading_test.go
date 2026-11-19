package network

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The loading screen. A client that has joined but not yet sent Player
// Loaded — and, after a death, one waiting for its respawn — is one whose
// packets vanilla's ServerGamePacketListenerImpl refuses to act on: the
// movement shapes, the player action, the two use packets, the attack, the
// interact, the input and the player command all begin with a
// hasClientLoaded check that drops the packet whole. These tests drive the
// same refusals through a real connection: a join that stops short of
// Player Loaded (enterWorldLoading) finds every gated packet ignored, the
// same packet works once Player Loaded has gone out, and it works again
// once the sixty-tick grace has run out without one.
//
// Every test here stops the main loop before it joins anyone. The grace is
// then a counter that only moves when the test says so — the refusals
// cannot run out under a slow machine, and the tests that want it run out
// drive the survival pass by hand rather than waiting on the clock.

// loadingGraceTicks is the sixty ticks of
// ServerGamePacketListenerImpl.clientLoadedTimeoutTimer, the wait a join or
// a respawn starts and Player Loaded cuts short.
const loadingGraceTicks = 60

// runOutTheLoadingGrace ticks the survival pass the grace's sixty ticks, the
// countdown ServerGamePacketListenerImpl.tickClientLoadTimeout runs. The
// main loop is stopped in these tests, so the ticks only move when this
// says so, and the player whose grace it is has said nothing.
func runOutTheLoadingGrace(t *testing.T, srv *server.Server) {
	t.Helper()
	for range loadingGraceTicks {
		srv.TickSurvival()
	}
}

// TestALoadingPlayerCannotMove checks the loading screen's oldest rule: a
// client still behind it is not moved by its own movement packets. All four
// shapes of the move packet - position with rotation, position, rotation,
// the flags alone - are dropped whole while the grace runs, and the same
// packets move the player once Player Loaded has gone out or the sixty
// ticks have gone by without one.
func TestALoadingPlayerCannotMove(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	// The loading screen holds the player where the join put them,
	// whatever the four packets say.
	mover := connect()
	mover.Login("Mover")
	mover.Configure()
	mover.EnterWorldLoading("Mover")
	spawn := srv.PlayerPosition("Mover")

	mover.MoveToGround(3.5, 64, 3.5, true)
	mover.SendPosition(4.5, 64, 4.5, true)
	mover.SendRotation(90, 10, false)
	mover.SendMovementFlags(true)
	mover.Settle()
	if got := srv.PlayerPosition("Mover"); got != spawn {
		t.Errorf("position behind the loading screen = %v, want the spawn %v", got, spawn)
	}

	// Player Loaded opens the world: the same packets move the player.
	mover.MarkLoaded()
	mover.MoveToGround(5.5, 64, 5.5, true)
	mover.Settle()
	if got := srv.PlayerPosition("Mover"); got.X != 5.5 || got.Y != 64 || got.Z != 5.5 {
		t.Errorf("position after Player Loaded = %v, want (5.5, 64, 5.5)", got)
	}

	// The grace running out opens it too: a client that never sent Player
	// Loaded is believed once the sixty ticks are gone.
	later := connect()
	later.Login("Later")
	later.Configure()
	later.EnterWorldLoading("Later")
	runOutTheLoadingGrace(t, srv)

	later.SendRotation(45, 5, false)
	later.SendMovementFlags(true)
	later.SendPosition(6.5, 64, 6.5, true)
	later.Settle()
	if got := srv.PlayerPosition("Later"); got.X != 6.5 || got.Y != 64 || got.Z != 6.5 {
		t.Errorf("position after the grace ran out = %v, want (6.5, 64, 6.5)", got)
	}
	if got := srv.PlayerPosition("Later"); got.Yaw != 45 || !got.OnGround {
		t.Errorf("rotation and on-ground after the grace = (%v, %v), want (45, true)",
			got.Yaw, got.OnGround)
	}
}

// TestALoadingPlayerCannotPlace checks the placement: a Use Item On from
// behind the loading screen is dropped unacknowledged — the sequence in it
// goes unacked — and places once the loading screen has closed by either
// road.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestALoadingPlayerCannotPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	placer := connect()
	placer.Login("Placer")
	placer.Configure()
	placer.EnterWorldLoading("Placer")
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no stone")
	}
	placer.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})

	placer.PlaceBlock(0, 64, 3, protocol.FaceTop)
	placer.Settle()
	if got := srv.BlockAt(0, 64, 3); got != 0 {
		t.Errorf("block after a placement behind the loading screen = %d, want the spot still empty", got)
	}
	placer.ExpectNothing(srv)

	placer.MarkLoaded()
	placer.PlaceBlock(0, 64, 3, protocol.FaceTop)
	placer.SettleKeeping()
	srv.TickSurvival()
	if change := placer.ExpectBlockChange(); change.State != uint16(stone) {
		t.Errorf("block after a loaded placement = %d, want stone (%d)", change.State, stone)
	}
	if got := srv.BlockAt(0, 64, 3); got != uint16(stone) {
		t.Errorf("block after a loaded placement in the world = %d, want stone (%d)", got, stone)
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	later.EnterWorldLoading("Later")
	later.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	runOutTheLoadingGrace(t, srv)

	later.PlaceBlock(0, 64, 4, protocol.FaceTop)
	later.SettleKeeping()
	srv.TickSurvival()
	if change := later.ExpectBlockChange(); change.State != uint16(stone) {
		t.Errorf("block after a placement past the grace = %d, want stone (%d)", change.State, stone)
	}
	if got := srv.BlockAt(0, 64, 4); got != uint16(stone) {
		t.Errorf("block after a placement past the grace in the world = %d, want stone (%d)", got, stone)
	}
}

// awaitRespawnSequence reads the standing-in-the-world sequence a respawn sends
// after the Respawn packet itself: the bars, the abilities, the place, the
// border, the clock, the spawn and the game event that lets the client's loading screen
// close. It is what both respawn tests here share.
func awaitRespawnSequence(t *testing.T, c *servertest.Client) {
	t.Helper()
	// What the death left on the wire is stepped over: a /kill's feedback
	// rides the mailbox behind the death.
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no Respawn arrived")
	}
	c.Expect(protocol.IDPlaySetHealth)
	c.Expect(protocol.IDPlaySetExperience)
	c.Expect(protocol.IDPlayAbilities)
	if pos := servertest.ExpectTeleport(t, c); pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("respawn = %v, want the world spawn (0.5, 64, 0.5)", pos)
	}
	c.Expect(protocol.IDPlayInitializeBorder)
	c.Expect(protocol.IDPlayUpdateTime)
	c.Expect(protocol.IDPlaySpawnPosition)
	c.Expect(protocol.IDPlayGameEvent)
}

// TestDeathKeepsTheLoadingGateClosed checks the rest of vanilla's
// hasClientLoaded: a dead player counts as not loaded however long the join
// grace ran (markClientUnloadedAfterDeath), Player Loaded does not change
// that, and only the respawn restarts the wait (restartClientLoadTimerAfterRespawn)
// - so a dig behind the death screen is refused until the respawn's own
// loading screen has been closed.
func TestDeathKeepsTheLoadingGateClosed(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Doomed"))
	srv.StopTick()

	c := connect()
	entityID := c.JoinAs("Doomed")

	c.ChatCommand("kill")
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Doomed")},
	}
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)

	// Player Loaded does not open the world to a dead player: vanilla's
	// markClientLoaded clears the timer alone, and waitingForRespawn holds
	// the gate either way.
	c.MarkLoaded()
	c.BreakBlock(0, 63, 5)
	c.Settle()
	if got := srv.BlockAt(0, 63, 5); got == 0 {
		t.Error("a dig behind the death screen broke the block")
	}
	c.ExpectNothing(srv)

	// The respawn arms the grace again: a dig is still refused until it
	// closes.
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	awaitRespawnSequence(t, c)

	// An efficiency V pickaxe's share of the stone is over the seven
	// tenths (34 / 1.5 / 30) the finished click breaks at, so the gate's
	// one taken dig needs no ticks of its own and the single tick the
	// test gives it flushes the acknowledgement; the death emptied the
	// slots, so the tool goes into the respawned player's hand.
	srv.GiveSlot("Doomed", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5))

	c.BreakBlock(0, 63, 5)
	c.Settle()
	if got := srv.BlockAt(0, 63, 5); got == 0 {
		t.Error("a dig behind the respawn's loading screen broke the block")
	}
	c.ExpectNothing(srv)

	// Player Loaded closes it, and the world opens. The pickaxe's dig
	// breaks its stone on the click itself once the player stands.
	c.MarkLoaded()
	c.MoveToGround(0.5, 64, 0.5, true)
	c.BreakBlock(0, 63, 5)
	c.SettleKeeping()
	srv.TickSurvival()
	if change := c.ExpectBlockChange(); change.State != 0 {
		t.Errorf("block after the loaded dig = %d, want air", change.State)
	}
	if got := srv.BlockAt(0, 63, 5); got != 0 {
		t.Errorf("block after the loaded dig in the world = %d, want air", got)
	}
}
