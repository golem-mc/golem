package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
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
