package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The movement schedule's tests: a mob's steps arrive on the rhythm its kind
// keeps rather than on every tick of the loop, an item's arrive on the
// slower rhythm the minecraft:item type keeps with the throw's own step
// carried at once, and a mob past its tracking range is nobody's to see
// until they walk close enough. The counters below read a connection the
// way a client does — keep-alives answered, the world's own traffic stepped
// over — and keep only what the entity they are watching was told.

// TestAMovingMobIsToldAboutOnVanillasSchedule checks the rhythm: a cow
// dropped from the air moves every tick of its fall, some seventeen of them,
// and the clients are still told about only about a third of those, the way
// vanilla's own schedule sends a mob's steps every third tick and no more. A
// packet a tick sends the fall as seventeen; the schedule sends it as seven.
func TestAMovingMobIsToldAboutOnVanillasSchedule(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)

	// Ten blocks of empty air: a fall of some seventeen ticks, every one of
	// them a step.
	mobID := srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	c.ExpectWithinTicks(srv, 2, protocol.IDPlaySpawnEntity)

	seen := c.CountEntityMovement(srv, mobID, 40)
	// One movement packet at most every third tick leaves six or seven for
	// the fall, with the landing's on-ground flip forcing one more; a packet
	// a tick would send seventeen.
	if seen.Positions < 2 {
		t.Errorf("the falling cow was told about %d times, want the fall's half dozen or so", seen.Positions)
	}
	if seen.Positions > 10 {
		t.Errorf("the falling cow was told about %d times, want at most one every third tick of its seventeen-tick fall, not one a tick",
			seen.Positions)
	}
}

// TestAnItemIsToldAboutOnItsOwnRhythm checks the minecraft:item type's
// slower one: an item is told about at most every twentieth tick — a fall of
// several ticks reaches its watcher as a single packet — with the throw's
// own step carried at once, the velocity of the throw being the impulse the
// first tracked tick has something real to send.
func TestAnItemIsToldAboutOnItsOwnRhythm(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dropper") // creative, so the drop is nobody's to pick back up
	c.Settle()
	servertest.HoldTicks(t, srv)

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	servertest.DropHeld(c, 0, 64, 0)

	drop := servertest.ParseItemSpawn(c.ExpectWithinTicks(srv, 2, protocol.IDPlaySpawnEntity))
	c.ReadDropStack(&drop)
	if drop.EntityID == 0 {
		t.Fatal("the dropped stone was not spawned")
	}

	// The throw's step arrives with the next tick, not with the twentieth.
	c.WaitForFirstMovement(srv, drop.EntityID, 2)

	// And the next hundred ticks hold no more than one every twentieth tick
	// plus the landing's catch-up and the occasional forced position, nothing
	// like the one-a-tick stream a falling item used to make. The sixth
	// packet is the landing's catch-up.
	seen := c.CountEntityMovement(srv, drop.EntityID, 100)
	if seen.Positions > 6 {
		t.Errorf("the item was told about %d times in a hundred ticks, want at most one every twentieth tick (%d movement ticks in the window)",
			seen.Positions, 100/20)
	}
}

// isAnimalSpawn picks out a spawn of one of the animals this server
// simulates. A player's body arriving is another player joining, which is
// none of the tracking assertions' business.
func isAnimalSpawn(id int32, data []byte) bool {
	if id != protocol.IDPlaySpawnEntity {
		return false
	}
	r := protocol.NewReader(data)
	r.ReadVarInt() // entity ID
	r.ReadUUID()
	switch r.ReadVarInt() {
	case protocol.EntityTypeChicken, protocol.EntityTypeCow,
		protocol.EntityTypePig, protocol.EntityTypeSheep:
		return true
	}
	return false
}

// expectNoMobSpawn runs n ticks and the barrier behind them, failing if a
// spawn of one of the simulated animals arrives.
func expectNoMobSpawn(t *testing.T, c *servertest.Client, srv *server.Server, n int) {
	t.Helper()
	c.ExpectNoneWithinTicks(srv, n, isAnimalSpawn)
}

// waitForMobSpawn reads until a spawn of one of the simulated animals
// arrives within n ticks, stepping over the background and past any player
// body that joins on the way, and returns its data.
func waitForMobSpawn(t *testing.T, c *servertest.Client, srv *server.Server, n int) []byte {
	t.Helper()
	var got []byte
	if !c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if isAnimalSpawn(id, data) {
			got = data
			return true
		}
		return false
	}) {
		t.Fatalf("no mob spawn arrived within %d ticks", n)
	}
	return got
}

// TestAMobBeyondItsTrackingRangeIsNotShown checks the showing: a cow a
// player's whole sight away is not spawned for them at all — not by the
// mob's arrival, not by anything else — and is brought to them when they
// walk within the range the kind is seen from, and taken back out when they
// walk past it again. The view distance is set short so the range that binds
// is the shorter of the two: with four chunks of sight, a mob is seen from
// sixty-four blocks. One player walks out far first, so that the other, who
// stays at the spawn the cow is planted beside, is the one holding its
// chunk in the world.
func TestAMobBeyondItsTrackingRangeIsNotShown(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4

	srv, connect := servertest.Start(t, cfg)
	wanderer := connect().Join("Wanderer")

	// Seven and a half chunks east: past the sixty-four blocks the kind is
	// seen from, and past the chunks being sent them.
	wanderer.MoveToGround(120.5, 64, 5.5, true)
	wanderer.Settle()

	watcher := connect().Join("Watcher")

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	watcher.ExpectMobSpawn(protocol.EntityTypeCow)

	expectNoMobSpawn(t, wanderer, srv, 20)

	// Twenty-five blocks out now, well inside the sixty-four: the next
	// movement tick brings the cow to them, body, state and pace.
	wanderer.MoveToGround(40.5, 64, 5.5, true)
	data := waitForMobSpawn(t, wanderer, srv, 100)
	brought := protocol.NewReader(data)
	if got := brought.ReadVarInt(); got != mobID {
		t.Errorf("the spawn brought to the player has entity %d, want the cow's %d", got, mobID)
	} else if brought.ReadUUID() == (uuid.UUID{}) {
		t.Error("the spawn brought to the player carried no identity")
	} else if got := brought.ReadVarInt(); got != protocol.EntityTypeCow {
		t.Errorf("the spawn brought to the player is of type %d, want the cow's %d", got, protocol.EntityTypeCow)
	}

	// And back out past the range again, the cow is taken back out of their
	// world the same way it came in — beside the other player's body, which
	// the walk takes out of their world at the same time.
	wanderer.MoveToGround(120.5, 64, 5.5, true)
	servertest.AwaitRemovalsOfTicks(t, wanderer, srv, 1200, mobID)
}

// expectNoPairingChange fails if the client loses or gains any of the ids
// within n ticks: a pairing that keeps — a teleport within range — is
// neither taken out nor sent again.
func expectNoPairingChange(t *testing.T, srv *server.Server, c *servertest.Client, n int, ids ...int32) {
	t.Helper()
	check := func(id int32, data []byte) {
		t.Helper()
		switch id {
		case protocol.IDPlayRemoveEntities:
			for _, gone := range servertest.RemovedIDs(t, data) {
				if slices.Contains(ids, gone) {
					t.Fatalf("entity %d was removed, want the pairing kept", gone)
				}
			}
		case protocol.IDPlaySpawnEntity:
			if spawned := protocol.NewReader(data).ReadVarInt(); slices.Contains(ids, spawned) {
				t.Fatalf("entity %d was spawned again, want the pairing kept", spawned)
			}
		}
	}
	c.SettleAndCollect(check)
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		check(id, data)
		return false
	})
}

// TestPlayersAreRepairedAfterAFarTeleport checks the pairing's reach for
// players, the same question the mob's is asked above: a teleport far out
// of the tracking range takes each body out of the other's world, and the
// teleport back re-sends each body whole, both directions, without a word
// to the tab list. A teleport within range is none of that: no removal, no
// spawn, and the step after it still measured from where the teleport
// landed.
func TestPlayersAreRepairedAfterAFarTeleport(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4 // the sixty-four blocks of sight the range binds to
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	mover := connect()
	moverID := mover.JoinAs("Mover")

	stayer := connect()
	stayerID := stayer.JoinAs("Stayer")
	mover.ExpectPlayerAdded("Stayer")
	mover.ExpectAnnouncement("multiplayer.player.joined", "Stayer")

	// Twelve and a half chunks off, in the same dimension: past the
	// sixty-four blocks the four chunks of sight bind the player's range
	// to, and past the chunks being sent either way.
	mover.ChatCommand("tp @s 200.5 64 200.5")
	servertest.AwaitCommandFeedbackText(t, mover, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Mover"),
		protocol.Text{Text: "200.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "200.500000"})
	servertest.ExpectTeleport(t, mover)
	mover.MoveToGround(200.5, 64, 200.5, true)

	// The teleport took the players out of each other's worlds, and only
	// that: the profiles stay where the tab list has them.
	servertest.AwaitRemovalsOfTicks(t, mover, srv, 1200, stayerID)
	servertest.AwaitRemovalsOfTicks(t, stayer, srv, 1200, moverID)
	expectNoPacketWithin(t, srv, mover, 10, protocol.IDPlayPlayerInfoRemove)
	expectNoPacketWithin(t, srv, stayer, 10, protocol.IDPlayPlayerInfoRemove)

	// And back to where they stand together.
	mover.ChatCommand("tp @s 0.5 64 0.5")
	servertest.AwaitCommandFeedbackText(t, mover, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Mover"),
		protocol.Text{Text: "0.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "0.500000"})
	servertest.ExpectTeleport(t, mover)
	mover.MoveToGround(0.5, 64, 0.5, true)

	// Each side sees the other's body again, re-sent whole — the spawn,
	// the state it carries and what it holds and wears.
	mover.ExpectPlayerPaired(stayerID, "Stayer")
	stayer.ExpectPlayerPaired(moverID, "Mover")

	// A teleport within range is a position sync, not a repair: neither
	// side's pairing changes.
	mover.ChatCommand("tp @s 10.5 64 10.5")
	servertest.AwaitCommandFeedbackText(t, mover, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Mover"),
		protocol.Text{Text: "10.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "10.500000"})
	servertest.ExpectTeleport(t, mover)
	mover.MoveToGround(10.5, 64, 10.5, true)
	expectNoPairingChange(t, srv, mover, 10, stayerID)
	expectNoPairingChange(t, srv, stayer, 10, moverID)

	// The step that follows is the difference from where the teleport
	// landed, or the teleport would have left the tracking behind it. A
	// zero delta is the tracker's forced resend every 60 ticks, stepped
	// over. vanilla: ServerEntity.sendChanges
	mover.MoveTo(11.5, 64, 10.5, 0, 0)
	var dx, dy, dz int16
	for dx == 0 && dy == 0 && dz == 0 {
		step := protocol.NewReader(stayer.ExpectPlay(protocol.IDPlayEntityPosition))
		if got := step.ReadVarInt(); got != moverID {
			t.Fatalf("stepping entity = %d, want %d", got, moverID)
		}
		step.ReadVarInt() // properties
		dx, dy, dz = step.ReadI16(), step.ReadI16(), step.ReadI16()
	}
	if dx != 4096 || dy != 0 || dz != 0 {
		t.Errorf("step after the teleport = (%d, %d, %d), want (4096, 0, 0)", dx, dy, dz)
	}
}

// TestATravellerWhoLeavesMidPassIsNotPairedBack settles a tracking pass on a
// snapshot taken before a player crossed dimensions: the holder left in the
// old world must not be sent the traveller's body again, which the crossing's
// removal had already taken out.
func TestATravellerWhoLeavesMidPassIsNotPairedBack(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)

	holder := connect().Join("Holder")
	traveller := connect().Join("Traveller")
	travellerID := holder.ExpectPlayerAdded("Traveller")
	holder.Settle()
	traveller.Settle()

	// Hold the pass open after its snapshot while the traveller crosses: the
	// snapshot still has the traveller in the overworld, and the settle runs
	// after the crossing stored the nether and cleared the record.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.PlayerTrackBetween(func() {
		once.Do(func() { close(entered) })
		<-release
	})
	go func() {
		<-entered
		srv.MovePlayerToDimension("Traveller", world.NetherName, 8, 40, 8)
		close(release)
	}()

	// The holder gains nothing: the body left for the nether, and the
	// crossing's removal is the last the holder hears of it.
	ghost := false
	holder.PumpTicks(srv, 3, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == travellerID {
			ghost = true
		}
		return ghost
	})
	if ghost {
		t.Error("the holder was paired with the body of a player now in the nether")
	}
}

// TestAPairingMidStepKeepsTheBodiesPlace walks a mover into a viewer's
// tracking range while a pass is held open between deciding the pairing and
// sending it, steps the mover again in that window, and checks the viewer's
// copy: the pairing must place the body where the step left it, not one step
// behind where the pass decided.
func TestAPairingMidStepKeepsTheBodiesPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4 // the sixty-four blocks of sight the range binds to
	srv, connect := servertest.Start(t, cfg)

	viewer := connect().Join("Viewer")

	mover := connect()
	moverID := mover.JoinAs("Mover")
	viewer.ExpectPlayerAdded("Mover")

	// Twelve and a half chunks off: past the sixty-four blocks, and the tick
	// takes the body back out.
	mover.MoveTo(200.5, 64, 200.5, 0, 0)
	mover.Settle()
	servertest.AwaitRemovalsOfTicks(t, viewer, srv, 1200, moverID)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.PlayerSendBetween(func() {
		once.Do(func() { close(entered) })
		<-release
	})

	// Back into range: the pass decides the pairing and holds before sending.
	mover.MoveTo(40.5, 64, 0.5, 0, 0)
	<-entered

	// One more step while the pass is held: the body has moved a block on
	// from where the pairing was decided.
	mover.MoveTo(41.5, 64, 0.5, 0, 0)
	mover.Settle()
	close(release)

	x, ok := awaitSpawnAt(t, viewer, moverID)
	if !ok {
		t.Fatal("the viewer was never paired with the mover's body")
	}
	if x != 41.5 {
		t.Errorf("the paired body stands at x=%g, want 41.5 where the step left it", x)
	}
}

// TestAPairingMidUnsentStepKeepsTheBodiesPlace holds a pass open between
// deciding the pairing and sending it, then lets a mover report a step into
// the window after setting its position but before its movement record runs.
// The spawn takes the step in, and nothing after it doubles the step.
func TestAPairingMidUnsentStepKeepsTheBodiesPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4 // the sixty-four blocks of sight the range binds to
	srv, connect := servertest.Start(t, cfg)

	viewer := connect().Join("Viewer")

	mover := connect()
	moverID := mover.JoinAs("Mover")
	viewer.ExpectPlayerAdded("Mover")

	// Twelve and a half chunks off: past the sixty-four blocks, and the tick
	// takes the body back out.
	mover.MoveTo(200.5, 64, 200.5, 0, 0)
	mover.Settle()
	servertest.AwaitRemovalsOfTicks(t, viewer, srv, 1200, moverID)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.PlayerSendBetween(func() {
		once.Do(func() { close(entered) })
		<-release
	})

	// Back into range: the pass decides the pairing and holds before sending.
	mover.MoveTo(40.5, 64, 0.5, 0, 0)
	mover.Settle()
	<-entered

	// A step whose position is set but whose telling is held: the baseline
	// the pass reads is still where the pairing was decided.
	moveEntered := make(chan struct{})
	moveRelease := make(chan struct{})
	srv.PlayerMoveBetween("Mover", func() {
		close(moveEntered)
		<-moveRelease
	})
	mover.MoveTo(41.5, 64, 0.5, 0, 0)
	<-moveEntered
	// Settle the pairing before the telling in flight is let go, so the
	// spawn reaches the viewer first and the step lands on it.
	close(release)
	x, ok := awaitSpawnAt(t, viewer, moverID)
	if !ok {
		t.Fatal("the viewer was never paired with the mover's body")
	}
	if x != 41.5 {
		t.Errorf("the paired body stands at x=%g, want 41.5, the step having been folded into the spawn", x)
	}
	close(moveRelease)
	viewer.ExpectNoneWithinTicks(srv, 2, func(packetID int32, data []byte) bool {
		return (packetID == protocol.IDPlayEntityPosition ||
			packetID == protocol.IDPlayEntityPositionRot ||
			packetID == protocol.IDPlayEntityRotation) &&
			protocol.NewReader(data).ReadVarInt() == moverID
	})
}

// TestAJoinMidStepKeepsTheBodiesPlace joins a player while a body's step is
// in flight: the join must record the mover before it spawns its body, and
// spawn from the baseline the step is measured from, so the step lands on the
// body the join made instead of being dropped behind it.
func TestAJoinMidStepKeepsTheBodiesPlace(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)

	mover := connect()
	moverID := mover.JoinAs("Mover")

	// Hold the join after the mover's body has been spawned for the joiner.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.PlayerJoinBetween(func() {
		once.Do(func() { close(entered) })
		<-release
	})

	joiner := connect()
	joiner.Login("Joiner")
	joiner.Configure()
	// Read up to the tab list, which the join sends before the body; the
	// server then blocks at the seam with the body already written.
	joiner.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
	<-entered

	// One step while the join is held: the body has moved on from where the
	// join spawned it.
	// The ground state stays the one the body spawned with, or its change
	// would ask for an absolute sync instead of a step.
	mover.MoveToGround(1.5, 64, 0.5, false)
	mover.Settle()
	close(release)

	x, ok := awaitSpawnAt(t, joiner, moverID)
	if !ok {
		t.Fatal("the joiner was never sent the mover's body")
	}
	if x != 0.5 {
		t.Errorf("the joined body stands at x=%g, want the 0.5 baseline the step is measured from", x)
	}

	// The schedule's periodic position packet may come first, carrying no
	// step; the one that follows holds the step taken during the join.
	var dx, dy, dz int16
	found := false
	for packetID, data := range joiner.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlayEntityPosition {
			continue
		}
		step := protocol.NewReader(data)
		if got := step.ReadVarInt(); got != moverID {
			t.Errorf("stepping entity = %d, want %d", got, moverID)
		}
		step.ReadVarInt() // properties
		dx, dy, dz = step.ReadI16(), step.ReadI16(), step.ReadI16()
		if dx != 0 || dy != 0 || dz != 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the step taken during the join never reached the joiner")
	}
	if dx != 4096 || dy != 0 || dz != 0 {
		t.Errorf("step during the join = (%d, %d, %d), want (4096, 0, 0)", dx, dy, dz)
	}
}

// TestAJoinDoesNotSpawnABodyAnArrivalAlreadyQueued joins a player while
// another player arrives in their dimension: the arrival broadcast queues and
// records the body, so the join must not also write its own spawn, or the
// client draws a second body no removal ever takes away.
func TestAJoinDoesNotSpawnABodyAnArrivalAlreadyQueued(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)

	arriver := connect()
	arriverID := arriver.JoinAs("Arriver")

	// Into the nether, so coming home is the arrival that races the join.
	srv.MovePlayerToDimension("Arriver", world.NetherName, 8, 40, 8)
	arriver.Settle()

	// Hold the join after its tab list and before it spawns any bodies.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.PlayerJoinBodiesBetween(func() {
		once.Do(func() { close(entered) })
		<-release
	})

	joiner := connect()
	joiner.Login("Joiner")
	joiner.Configure()
	<-entered

	// The arriver comes back into the overworld while the join is held: the
	// arrival broadcast queues its body into the joiner's mailbox and the
	// record says the joiner holds it. Both flush once the join releases.
	srv.MovePlayerToDimension("Arriver", world.DimensionName, 0.5, 64, 0.5)
	close(release)

	// The queued spawn is written by the mailbox, which races the chunk
	// stream's direct writes, so it can land after any batch: the whole
	// join is read, then a settle, whose answer rides the same mailbox
	// behind it.
	spawns := 0
	count := func(id int32, data []byte) {
		if id == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == arriverID {
			spawns++
		}
	}
	joiner.ReadJoin(count)
	joiner.SettleAndCollect(count)
	if spawns != 1 {
		t.Errorf("the joiner was sent the arriver's body %d times, want one", spawns)
	}
}

// awaitSpawnAt reads until a Spawn Entity for id arrives and returns the x it
// places the body at.
func awaitSpawnAt(t *testing.T, c *servertest.Client, id int32) (float64, bool) {
	t.Helper()
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		t.Helper()
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			continue
		}
		r.ReadUUID()
		r.ReadVarInt()
		return r.ReadF64(), true
	}
	return 0, false
}

// expectNoPacketWithin fails if a packet of the given ID arrives within n ticks.
func expectNoPacketWithin(t *testing.T, srv *server.Server, c *servertest.Client, n int, unwanted int32) {
	t.Helper()
	c.ExpectNoneWithinTicks(srv, n, servertest.PacketIs(unwanted))
}

// TestAShulkerBulletIsToldAboutEveryThirdTick checks a bullet keeps the
// builder's default schedule of three ticks rather than a snowball's ten, so
// a client sees its server-side homing as a curve.
// vanilla: EntityTypes.SHULKER_BULLET
func TestAShulkerBulletIsToldAboutEveryThirdTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	srv.SetSpawning(false)
	srv.SetDifficulty(rules.DifficultyNormal) // Peaceful discards a bullet
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:shulker_bullet 5.5 100 5.5")
	id := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlaySpawnEntity)).ReadVarInt()

	// Thirty ticks on a three-tick schedule are ten updates; on ten, three.
	if seen := c.CountEntityMovement(srv, id, 30); seen.Positions < 8 {
		t.Errorf("the bullet was told about %d times in 30 ticks, want one every third tick", seen.Positions)
	}
}

// TestADropKeyThrowIsShownInTheTickItIsThrown: with no listener the thrown
// item's spawn goes out with the packet's handling, not at the tracker's
// next settle.
// vanilla: ServerPlayer.drop
func TestADropKeyThrowIsShownInTheTickItIsThrown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Thrower")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Thrower", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond"), Count: 2})
	c.DropKey(false)
	c.AwaitSpawn(protocol.EntityTypeItem)
}
