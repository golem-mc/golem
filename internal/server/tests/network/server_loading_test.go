package network

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

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

// TestALoadingPlayerCannotDig checks the dig: a player action from behind
// the loading screen is dropped before it is even acknowledged - no block
// change sequence answered, no block update sent, the world left as it was
// - which is what vanilla's handlePlayerAction does, its hasClientLoaded
// check standing ahead of everything the packet asks for.
func TestALoadingPlayerCannotDig(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	digger := connect()
	digger.Login("Digger")
	digger.Configure()
	digger.EnterWorldLoading("Digger")

	ground := srv.BlockAt(0, 63, 1)
	if ground == 0 {
		t.Fatal("the block to dig is already air")
	}
	digger.BreakBlock(0, 63, 1)
	digger.Settle()
	if got := srv.BlockAt(0, 63, 1); got != ground {
		t.Errorf("block after a dig behind the loading screen = %d, want it still standing", got)
	}
	digger.ExpectNothing(srv)

	// Player Loaded, and the same dig breaks the block.
	digger.MarkLoaded()
	digger.BreakBlock(0, 63, 1)
	digger.SettleKeeping()
	srv.TickSurvival()
	if change := digger.ExpectBlockChange(); change.State != 0 {
		t.Errorf("block after a loaded dig = %d, want air", change.State)
	}
	if got := srv.BlockAt(0, 63, 1); got != 0 {
		t.Errorf("block after a loaded dig in the world = %d, want air", got)
	}

	// The grace runs out, and a client that said nothing digs too.
	later := connect()
	later.Login("Later")
	later.Configure()
	later.EnterWorldLoading("Later")
	runOutTheLoadingGrace(t, srv)

	later.BreakBlock(0, 63, 2)
	later.SettleKeeping()
	srv.TickSurvival()
	if change := later.ExpectBlockChange(); change.State != 0 {
		t.Errorf("block after a dig past the grace = %d, want air", change.State)
	}
	if got := srv.BlockAt(0, 63, 2); got != 0 {
		t.Errorf("block after a dig past the grace in the world = %d, want air", got)
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

// TestALoadingPlayerCannotUse checks the Use Item packet: a right-click from
// behind the loading screen starts nothing - vanilla's handleUseItem
// refuses it ahead of the sequence ack and the use both - and the held
// bread begins its meal once the loading screen has closed by either road.
func TestALoadingPlayerCannotUse(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	eater := connect()
	eater.Login("Eater")
	eater.Configure()
	eater.EnterWorldLoading("Eater")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:bread"), Count: 1})
	// A meal needs the appetite for one: vanilla refuses to eat on a full
	// bar, so the test takes some of the join's twenty off first.
	srv.SetFood("Eater", 10)

	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if srv.Eating("Eater") {
		t.Error("a use behind the loading screen began a meal")
	}
	eater.ExpectNothing(srv)

	eater.MarkLoaded()
	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins once Player Loaded has gone out")
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	later.EnterWorldLoading("Later")
	srv.GiveSlot("Later", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:bread"), Count: 1})
	srv.SetFood("Later", 10)
	runOutTheLoadingGrace(t, srv)

	later.UseItemRightClicks(protocol.HandMain)
	later.Settle()
	if !srv.Eating("Later") {
		t.Fatal("the meal begins once the grace has run out")
	}
}

// TestALoadingPlayerCannotAttack checks the Attack packet: a swing from
// behind the loading screen is refused before any target is looked up -
// vanilla's handleAttack is the hasClientLoaded check and the spectator
// one, in that order - and the same swing lands once the loading screen has
// closed by either road.
func TestALoadingPlayerCannotAttack(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	fighter := connect()
	fighter.Login("Fighter")
	fighter.Configure()
	fighterID := fighter.EnterWorldLoading("Fighter")

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := fighter.ExpectMobSpawn(protocol.EntityTypeCow)

	fighter.Attack(cow.EntityID)
	fighter.Settle()
	fighter.ExpectNothing(srv)

	fighter.MarkLoaded()
	fighter.MoveToGround(2.5, 64, 5.5, true)
	fighter.Attack(cow.EntityID)
	fighter.ExpectMobDamageEvent(cow.EntityID, "minecraft:player_attack", fighterID+1)

	// The grace running out lets the silent client hit too. Its join comes
	// after the loaded swing, so the damage event that one broadcast is
	// not waiting in its buffer to be mistaken for its own; and the cow it
	// hits is a fresh one, the tick loop being stopped and the half-second
	// of invulnerability the first hit bought never counting down for the
	// second.
	later := connect()
	later.Login("Later")
	later.Configure()
	laterID := later.EnterWorldLoading("Later")
	runOutTheLoadingGrace(t, srv)
	srv.SpawnMob("minecraft:cow", 7.5, 64, 5.5)
	second := later.ExpectMobSpawn(protocol.EntityTypeCow)
	later.MoveToGround(3.5, 64, 5.5, true)
	later.Attack(second.EntityID)
	later.ExpectMobDamageEvent(second.EntityID, "minecraft:player_attack", laterID+1)
}

// TestALoadingPlayerCannotInteract checks the Interact packet: a right-click
// on an entity from behind the loading screen does nothing to it - vanilla's
// handleInteract refuses the packet whole - and the same click on the same
// cow fills the bucket once the loading screen has closed by either road.
func TestALoadingPlayerCannotInteract(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	bucket, ok := registry.ItemID("minecraft:bucket")
	if !ok {
		t.Fatal("the item registry has no bucket")
	}
	milk, ok := registry.ItemID("minecraft:milk_bucket")
	if !ok {
		t.Fatal("the item registry has no milk bucket")
	}

	milker := connect()
	milker.Login("Milker")
	milker.Configure()
	milker.EnterWorldLoading("Milker")
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})

	// The cow stands within the entity interaction's reach of the milker
	// at spawn: vanilla refuses a use past the reach, and a player behind
	// the loading screen cannot be moved to close the distance.
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow := milker.ExpectMobSpawn(protocol.EntityTypeCow)

	milker.UseOn(cow.EntityID)
	milker.Settle()
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket {
		t.Errorf("the bucket behind the loading screen = item %d, want it still the bucket (%d)",
			got.ItemID, bucket)
	}
	milker.ExpectNothing(srv)

	milker.MarkLoaded()
	milker.UseOn(cow.EntityID)
	slot, stack := milker.ExpectContainerSlot()
	if slot < int16(player.HotbarSlot) || slot > int16(player.HotbarSlot+8) {
		milker.T.Errorf("the milk went to slot %d, want a hotbar slot", slot)
	}
	if stack.ItemID != milk || stack.Count != 1 {
		milker.T.Errorf("the filled bucket = %+v, want one milk bucket (item %d)", stack, milk)
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != milk {
		milker.T.Errorf("the server's slot holds item %d, want the milk bucket %d", got.ItemID, milk)
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	later.EnterWorldLoading("Later")
	srv.GiveSlot("Later", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	runOutTheLoadingGrace(t, srv)

	later.UseOn(cow.EntityID)
	if slot, stack := later.ExpectContainerSlot(); stack.ItemID != milk {
		later.T.Errorf("the filled bucket = %+v (slot %d), want one milk bucket (item %d)", stack, slot, milk)
	}
}

// TestALoadingPlayerCannotSneak checks the Player Input packet's sneak key:
// vanilla's handlePlayerInput keeps the packet's input either way but takes
// the shift key into the player's state only once the loading screen has
// closed, so the crouch the others would be shown waits for it - by Player
// Loaded or by the sixty ticks.
func TestALoadingPlayerCannotSneak(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	watcher := connect().Join("Watcher")

	actor := connect()
	actor.Login("Actor")
	actor.Configure()
	actorID := actor.EnterWorldLoading("Actor")
	if got := watcher.ExpectPlayerAdded("Actor"); got != actorID {
		t.Fatalf("entity ID disagreement: watcher saw %d, actor is %d", got, actorID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	actor.SendSneak(true)
	actor.Settle()
	watcher.ExpectNothing(srv)

	actor.MarkLoaded()
	actor.SendSneak(true)
	sneaking := watcher.ExpectMetadata()
	if sneaking.EntityID != actorID {
		t.Errorf("sneaking entity = %d, want %d", sneaking.EntityID, actorID)
	}
	if sneaking.Flags&protocol.EntityFlagSneaking == 0 {
		t.Errorf("entity flags = %#02x, want the sneaking bit set", sneaking.Flags)
	}
	if sneaking.Pose != protocol.PoseSneaking {
		t.Errorf("pose = %d, want %d (sneaking)", sneaking.Pose, protocol.PoseSneaking)
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	laterID := later.EnterWorldLoading("Later")
	if got := watcher.ExpectPlayerAdded("Later"); got != laterID {
		t.Fatalf("entity ID disagreement: watcher saw %d, later is %d", got, laterID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Later")
	runOutTheLoadingGrace(t, srv)

	later.SendSneak(true)
	lateSneak := watcher.ExpectMetadata()
	if lateSneak.EntityID != laterID {
		t.Errorf("sneaking entity = %d, want %d", lateSneak.EntityID, laterID)
	}
	if lateSneak.Flags&protocol.EntityFlagSneaking == 0 {
		t.Errorf("entity flags = %#02x, want the sneaking bit set", lateSneak.Flags)
	}
}

// TestALoadingPlayerCannotSprint checks the Player Command packet's sprint:
// vanilla's handlePlayerCommand refuses the whole packet behind the loading
// screen, so the run the others would be shown waits for it - by Player
// Loaded or by the sixty ticks.
func TestALoadingPlayerCannotSprint(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	watcher := connect().Join("Watcher")

	actor := connect()
	actor.Login("Actor")
	actor.Configure()
	actorID := actor.EnterWorldLoading("Actor")
	if got := watcher.ExpectPlayerAdded("Actor"); got != actorID {
		t.Fatalf("entity ID disagreement: watcher saw %d, actor is %d", got, actorID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	actor.SendSprint(actorID, true)
	actor.Settle()
	watcher.ExpectNothing(srv)

	actor.MarkLoaded()
	actor.SendSprint(actorID, true)
	sprinting := watcher.ExpectMetadata()
	if sprinting.EntityID != actorID {
		t.Errorf("sprinting entity = %d, want %d", sprinting.EntityID, actorID)
	}
	if sprinting.Flags&protocol.EntityFlagSprinting == 0 {
		t.Errorf("entity flags = %#02x, want the sprinting bit set", sprinting.Flags)
	}

	later := connect()
	later.Login("Later")
	later.Configure()
	laterID := later.EnterWorldLoading("Later")
	if got := watcher.ExpectPlayerAdded("Later"); got != laterID {
		t.Fatalf("entity ID disagreement: watcher saw %d, later is %d", got, laterID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Later")
	runOutTheLoadingGrace(t, srv)

	later.SendSprint(laterID, true)
	lateSprint := watcher.ExpectMetadata()
	if lateSprint.EntityID != laterID {
		t.Errorf("sprinting entity = %d, want %d", lateSprint.EntityID, laterID)
	}
	if lateSprint.Flags&protocol.EntityFlagSprinting == 0 {
		t.Errorf("entity flags = %#02x, want the sprinting bit set", lateSprint.Flags)
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

// TestTheRespawnGraceRunsOut checks the respawn's grace the way the join's
// is checked: a respawned client that never sends Player Loaded is believed
// once the sixty ticks have gone by.
func TestTheRespawnGraceRunsOut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Returner"))
	srv.StopTick()

	c := connect()
	entityID := c.JoinAs("Returner")

	c.ChatCommand("kill")
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Returner")},
	}
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)

	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	awaitRespawnSequence(t, c)

	// An efficiency V pickaxe's share of the stone is over the seven
	// tenths (34 / 1.5 / 30) the finished click breaks at, so the gate's
	// one taken dig needs no ticks of its own and the single tick the
	// test gives it flushes the acknowledgement; the death emptied the
	// slots, so the tool goes into the respawned player's hand.
	srv.GiveSlot("Returner", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5))

	// The respawn armed the grace again, so the dig behind its loading
	// screen is refused before the sixty ticks have run. The block is
	// within the block interaction's reach of the spawn the respawn put
	// the player back at: vanilla refuses a dig past the reach, and a
	// player behind the loading screen cannot be moved to close it.
	c.BreakBlock(0, 63, 3)
	c.Settle()
	if got := srv.BlockAt(0, 63, 3); got == 0 {
		t.Error("a dig behind the respawn's loading screen broke the block")
	}
	c.ExpectNothing(srv)

	runOutTheLoadingGrace(t, srv)
	// The pickaxe's dig breaks its stone on the click itself once the
	// player stands.
	c.MoveToGround(0.5, 64, 0.5, true)
	c.BreakBlock(0, 63, 3)
	c.SettleKeeping()
	srv.TickSurvival()
	if change := c.ExpectBlockChange(); change.State != 0 {
		t.Errorf("block after the grace past the respawn = %d, want air", change.State)
	}
	if got := srv.BlockAt(0, 63, 3); got != 0 {
		t.Errorf("block after the grace past the respawn in the world = %d, want air", got)
	}
}
