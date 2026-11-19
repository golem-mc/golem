package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// Tracking beyond the movement schedule: per-player removals, forgetting
// on a dimension change, what an entry into range carries, and the removal
// of a body whose life the tick ends.

// awaitShownIds reads until a Spawn Entity of the expected type has arrived
// for every id wanted, within the given ticks.
func awaitShownIds(t *testing.T, c *servertest.Client, srv *server.Server, ticks int, want map[int32]int32) {
	t.Helper()
	missing := maps.Clone(want)
	c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		kind := r.ReadVarInt()
		if err := r.Err(); err != nil {
			t.Fatalf("spawn entity: %v", err)
		}
		if expect, wait := missing[entityID]; wait {
			if kind != expect {
				t.Fatalf("spawn for entity %d is of type %d, want %d", entityID, kind, expect)
			}
			delete(missing, entityID)
		}
		return len(missing) == 0
	})
	if len(missing) > 0 {
		t.Fatalf("no spawn for %v arrived within %d ticks", missing, ticks)
	}
}

// TestABodyLeavingOnePlayersRangeIsRemovedForThatPlayerAlone walks one
// player away from an orb, a primed TNT, a falling block and a stuck arrow:
// the leaver is told each is removed, the player who stays is not.
func TestABodyLeavingOnePlayersRangeIsRemovedForThatPlayerAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Leaver"))
	leaver := connect().Join("Leaver")
	keeper := connect().Join("Keeper")

	// The orb lies past the eight blocks it follows a player from, the
	// fuse outlasts the test, and the sand starts at the top of the world
	// so its landing's own removal comes long after the keeper's check.
	orbID := srv.SpawnOrb(4.5, 64, 30.5, 1)
	tntID := srv.SpawnTNT(6.5, 64, 4.5, 400)
	arrowID := srv.SpawnStuckArrow("disallowed", 2.5, 64.5, 6.5)
	leaver.Settle()
	keeper.Settle()

	leaver.ChatCommand("setblock 5 319 3 sand")
	fallingID := leaver.AwaitFallingSpawn().EntityID
	keeper.AwaitFallingSpawn()

	// 120 blocks east is out of the 64 blocks four chunks of sight reach.
	// No settle here: it would drop the removals the move brings.
	leaver.MoveToGround(120.5, 64, 0.5, true)

	servertest.AwaitRemovalsOfTicks(t, leaver, srv, 40, orbID, tntID, arrowID, fallingID)
	servertest.ExpectNoRemovalOfTicks(t, keeper, srv, 10, orbID, tntID, arrowID, fallingID)
}

// TestADimensionChangeForgetsEveryBodyTheClientSaw sends a player through
// the nether and back: the Respawn cleared the client's world, so the orb,
// the stuck arrow and the egg in flight are shown again on the way home. A
// second player keeps their chunks, and so the bodies, loaded meanwhile.
func TestADimensionChangeForgetsEveryBodyTheClientSaw(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4

	srv, connect := servertest.Start(t, cfg)
	connect().Join("Holder")
	crosser := connect().Join("Crosser")
	crosser.Settle()
	// Held, so the egg's flight is counted in ticks and the crossing's
	// wall-clock chunk loading cannot let it land before the way home.
	servertest.HoldTicks(t, srv)

	// The orb past the eight blocks it follows a player from, and the egg
	// thrown from high enough that its flight outlasts the round trip.
	orbID := srv.SpawnOrb(10.5, 64, 4.5, 27)
	arrowID := srv.SpawnStuckArrow("disallowed", 3.5, 64.5, 6.5)
	eggID := srv.ThrowThrown(entity.ThrownEgg, 20.5, 310, 0.5, 0, -90, 0, 1)
	bodies := map[int32]int32{
		orbID:   protocol.EntityTypeExperienceOrb,
		arrowID: protocol.EntityTypeArrow,
		eggID:   protocol.EntityTypeEgg,
	}
	awaitShownIds(t, crosser, srv, 20, bodies)

	srv.MovePlayerToDimension("Crosser", world.NetherName, 88, 40, 88)
	srv.MovePlayerToDimension("Crosser", world.DimensionName, 0.5, 64, 0.5)

	awaitShownIds(t, crosser, srv, 20, bodies)
}

// TestAnOrbEnteringARangeCarriesItsWorthAlong walks a player up to an orb
// spawned out of their sight: its spawn comes with the metadata carrying its
// worth, which the client sizes the orb by.
func TestAnOrbEnteringARangeCarriesItsWorthAlong(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4

	srv, scout := servertest.Join(t, cfg, "Scout", servertest.FullSimulationDistance)

	scout.MoveToGround(100.5, 64, 100.5, true)
	scout.Settle()
	orbID := srv.SpawnOrb(4.5, 64, 4.5, 27)
	scout.Settle()

	// No settle after the move: it would drop the spawn the move brings.
	scout.MoveToGround(5.5, 64, 5.5, true)

	w := servertest.NewOrbWatcher(t, scout)
	var arrived bool
	w.ReadUntilTicks(srv, 200, func() bool {
		arrived = w.OrbIDs[orbID] == 27
		return arrived
	})
	if !arrived {
		t.Fatalf("the orb the scout walked in on carried no worth: %v", w.OrbIDs)
	}
}

// TestAnArrowThatHitsAMobIsRemoved shoots a cow: the arrow is discarded
// with the hit, and the client is told so.
func TestAnArrowThatHitsAMobIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher")

	cowID := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
	c.Settle()
	srv.MoveMob(cowID, 10.5, 0.5)
	arrowID := srv.ShootArrow(6.5, 64.5, 0.5, 2, 0, 0)

	var hurt, removed bool
	if c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent:
			hurt = hurt || protocol.NewReader(data).ReadVarInt() == cowID
		case protocol.IDPlayRemoveEntities:
			removed = removed || slices.Contains(servertest.RemovedIDs(t, data), arrowID)
		}
		return hurt && removed
	}) {
		return
	}
	t.Fatalf("cow hurt: %v, arrow removed: %v; want both", hurt, removed)
}

// TestAnArrowThatHitsAPlayerIsRemoved shoots the client's own player: the
// arrow is discarded with the hit, and the client is told so.
func TestAnArrowThatHitsAPlayerIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Target")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	arrowID := srv.ShootArrow(5.5, 65, 0.5, -2, 0, 0)

	var hurt, removed bool
	if c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			hurt = true
		case protocol.IDPlayRemoveEntities:
			removed = removed || slices.Contains(servertest.RemovedIDs(t, data), arrowID)
		}
		return hurt && removed
	}) {
		return
	}
	t.Fatalf("player hurt: %v, arrow removed: %v; want both", hurt, removed)
}

// TestAStuckArrowWhoseLifeRunsOutIsRemoved ages a stuck arrow to the end of
// its 1200 ticks: the despawn is told to the client.
func TestAStuckArrowWhoseLifeRunsOutIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	// Stand the shaft in the block at its own cell: in open air the
	// in-ground tick lets it fall and resets its life, so the despawn
	// never arrives.
	if !srv.SetBlock(2, 64, 6, servertest.MustState(t, "minecraft:stone", nil)) {
		t.Fatal("could not place the block holding the arrow")
	}
	arrowID := srv.SpawnStuckArrow("disallowed", 2.5, 64.5, 6.5)
	c.Settle()
	srv.AgeArrow(arrowID, 1195)

	servertest.AwaitRemovalsOfTicks(t, c, srv, 40, arrowID)
	if srv.ArrowAlive(arrowID) {
		t.Fatal("the arrow was removed for the client but is still alive")
	}
}

// awaitFallingStart reads until the metadata of the falling block id arrives
// and returns the start position it carries.
func awaitFallingStart(t *testing.T, srv *server.Server, c *servertest.Client, id int32) (x, y, z int32) {
	t.Helper()
	if !c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		t.Helper()
		if packetID != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		index, kind := r.ReadU8(), r.ReadVarInt()
		if index != protocol.MetadataIndexFallingBlockStartPos || kind != protocol.MetadataBlockPos {
			t.Fatalf("falling block metadata starts with field %d of type %d, want the start position", index, kind)
		}
		x, y, z = r.ReadPosition()
		if err := r.Err(); err != nil {
			t.Fatalf("falling block metadata: %v", err)
		}
		return true
	}) {
		t.Fatalf("no metadata for falling block %d arrived", id)
	}
	return x, y, z
}

// countSpawnsOf counts Spawn Entity packets carrying id over n ticks.
func countSpawnsOf(srv *server.Server, c *servertest.Client, id int32, n int) int {
	c.T.Helper()
	count := 0
	c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		c.T.Helper()
		if packetID != protocol.IDPlaySpawnEntity {
			return false
		}
		if protocol.NewReader(data).ReadVarInt() == id {
			count++
		}
		return false
	})
	return count
}

// TestARespawnedPlayerSeesThePlayerNearItAgain kills a player and respawns
// them in the same dimension, with another player standing near. The Respawn
// forgets the respawned client's tracking records, so the other player's
// body must be sent again; the onlooker, who never lost it, gets the new
// body once from the respawn's broadcast and no second time from the tick.
func TestARespawnedPlayerSeesThePlayerNearItAgain(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4 // the sixty-four blocks of sight the range binds to
	srv, connect := servertest.Start(t, cfg)

	dier := connect()
	dierID := dier.JoinAs("Dier")

	watcher := connect()
	watcherID := watcher.JoinAs("Watcher")
	if got := dier.ExpectPlayerAdded("Watcher"); got != watcherID {
		t.Fatalf("entity ID disagreement: dier saw %d, watcher is %d", got, watcherID)
	}
	dier.ExpectAnnouncement("multiplayer.player.joined", "Watcher")

	// Eight blocks off, well inside the player tracking range.
	watcher.MoveToGround(8.5, 64, 0.5, true)
	dier.Settle()
	watcher.Settle()

	if !srv.Hurt("Dier", 1000) {
		t.Fatal("the blow did not kill the dier")
	}
	dier.Settle()
	watcher.Settle()

	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	// The respawned player is shown the watcher's body again.
	if _, ok := awaitSpawnAt(t, dier, watcherID); !ok {
		t.Fatal("the respawned player was never shown the watcher's body again")
	}
	// The watcher is sent the new body once, by the respawn's own
	// broadcast, and not a second time by the tick.
	if n := countSpawnsOf(srv, watcher, dierID, 40); n != 1 {
		t.Errorf("the watcher was sent the dier's body %d times after the respawn, want one", n)
	}
}

// TestARespawnedPlayerSeesTheMobNearItAgain kills a player standing beside a
// cow and respawns them: the Respawn forgets the tracking records, so the
// cow must be paired again.
func TestARespawnedPlayerSeesTheMobNearItAgain(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4

	srv, c, _ := servertest.JoinAs(t, cfg, "Dier")

	// The cow stands near the world spawn the respawn returns to.
	mobID := srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if got := c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID; got != mobID {
		t.Fatalf("the cow spawned as entity %d, want %d", got, mobID)
	}
	c.Settle()

	if !srv.Hurt("Dier", 1000) {
		t.Fatal("the blow did not kill the dier")
	}
	c.Settle()

	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	if _, ok := awaitSpawnAt(t, c, mobID); !ok {
		t.Fatal("the respawned player was never shown the cow again")
	}
}

// TestARespawnedPlayerForgetsTheMobFarFromTheSpawn kills a player standing
// beside a cow and respawns them at a spawn point hundreds of blocks away. A
// same-dimension Respawn leaves the client's world standing, so the cow left
// at the death spot must be taken out by an explicit Remove Entities instead
// of standing there as a ghost. A second player stays by the cow, so its
// chunk stays loaded and the remove the dier must be sent is the respawn's
// own rather than the chunk unload's.
func TestARespawnedPlayerForgetsTheMobFarFromTheSpawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Dier"))

	dier := connect()
	dier.JoinAs("Dier")

	watcher := connect()
	watcherID := watcher.JoinAs("Watcher")
	if got := dier.ExpectPlayerAdded("Watcher"); got != watcherID {
		t.Fatalf("entity ID disagreement: dier saw %d, watcher is %d", got, watcherID)
	}
	dier.ExpectAnnouncement("multiplayer.player.joined", "Watcher")

	// The cow stands near the world spawn both players start at.
	mobID := srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if got := dier.ExpectMobSpawn(protocol.EntityTypeCow).EntityID; got != mobID {
		t.Fatalf("the cow spawned as entity %d, want %d", got, mobID)
	}
	if got := watcher.ExpectMobSpawn(protocol.EntityTypeCow).EntityID; got != mobID {
		t.Fatalf("the cow spawned as entity %d for the watcher, want %d", got, mobID)
	}
	dier.Settle()
	watcher.Settle()

	// The respawn comes back hundreds of blocks away, well out of the cow's
	// tracking range.
	dier.ChatCommand("spawnpoint Dier 600 64 600")
	dier.Settle()

	if !srv.Hurt("Dier", 1000) {
		t.Fatal("the blow did not kill the dier")
	}
	dier.Settle()
	watcher.Settle()

	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	servertest.AwaitRemovalsOfTicks(t, dier, srv, 40, mobID)
	if _, _, _, ok := srv.MobPos(mobID); !ok {
		t.Fatal("the cow was removed from the world, so the remove did not come from the respawn")
	}
}

// TestARespawnedPlayerForgetsThePlayerFarFromTheSpawn is the near-player
// respawn's counterpart: the dier comes back hundreds of blocks from the
// player they were standing beside, whose body must be removed from the
// dier's view rather than left as a ghost.
func TestARespawnedPlayerForgetsThePlayerFarFromTheSpawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Dier"))

	dier := connect()
	dier.JoinAs("Dier")

	watcher := connect()
	watcherID := watcher.JoinAs("Watcher")
	if got := dier.ExpectPlayerAdded("Watcher"); got != watcherID {
		t.Fatalf("entity ID disagreement: dier saw %d, watcher is %d", got, watcherID)
	}
	dier.ExpectAnnouncement("multiplayer.player.joined", "Watcher")

	watcher.MoveToGround(8.5, 64, 0.5, true)
	dier.Settle()
	watcher.Settle()

	dier.ChatCommand("spawnpoint Dier 600 64 600")
	dier.Settle()

	if !srv.Hurt("Dier", 1000) {
		t.Fatal("the blow did not kill the dier")
	}
	dier.Settle()
	watcher.Settle()

	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})

	servertest.AwaitRemovalsOfTicks(t, dier, srv, 40, watcherID)
}

// TestAFallingBlockCarriesWhereItsFallBegan checks the start position a
// falling block's pairing carries, vanilla's DATA_START_POS: to a player
// who saw the fall begin, and to one who walks into range mid-fall, whose
// spawn is no longer at the start.
func TestAFallingBlockCarriesWhereItsFallBegan(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Dropper"))
	dropper := connect().Join("Dropper")
	walker := connect().Join("Walker")
	walker.MoveToGround(120.5, 64, 0.5, true)
	walker.Settle()

	dropper.ChatCommand("setblock 5 319 3 sand")
	id := dropper.AwaitFallingSpawn().EntityID
	if x, y, z := awaitFallingStart(t, srv, dropper, id); x != 5 || y != 319 || z != 3 {
		t.Fatalf("start position on first sight = (%d, %d, %d), want (5, 319, 3)", x, y, z)
	}

	// Once the dropper sees it move, the fall is under way.
	if !dropper.Pump(servertest.ExpectTimeout, nil, func(packetID int32, data []byte) bool {
		return packetID == protocol.IDPlayEntityPosition && protocol.NewReader(data).ReadVarInt() == id
	}) {
		t.Fatal("the falling block never moved")
	}
	walker.MoveToGround(4.5, 64, 4.5, true)
	if spawn := walker.AwaitFallingSpawn(); spawn.EntityID != id || spawn.Y >= 319 {
		t.Fatalf("walker's spawn is entity %d at y %.1f, want %d below 319", spawn.EntityID, spawn.Y, id)
	}
	if x, y, z := awaitFallingStart(t, srv, walker, id); x != 5 || y != 319 || z != 3 {
		t.Fatalf("start position on entry = (%d, %d, %d), want (5, 319, 3)", x, y, z)
	}
}

// TestATippedArrowFadingInTheGroundPuffsAndLosesItsColour checks a tipped
// arrow's decay: after 600 ticks in the ground the watcher gets entity event
// 0 and then the colour metadata back at none.
func TestATippedArrowFadingInTheGroundPuffsAndLosesItsColour(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")

	if !srv.SetBlock(2, 64, 6, servertest.MustState(t, "minecraft:stone", nil)) {
		t.Fatal("could not place the block holding the arrow")
	}
	arrowID := srv.SpawnStuckArrow("disallowed", 2.5, 64.5, 6.5)
	srv.TipArrow(arrowID)
	c.Settle()
	srv.AgeArrow(arrowID, 598)

	puffed := false
	if c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == arrowID && r.ReadU8() == protocol.EntityStatusArrowEffectPuff {
				puffed = true
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != arrowID || r.ReadU8() != protocol.MetadataIndexArrowColor {
				return false
			}
			r.ReadVarInt()
			if got := r.ReadVarInt(); got != -1 {
				return false
			}
			if !puffed {
				t.Fatal("the colour reset arrived before entity event 0")
			}
			return true
		}
		return false
	}) {
		return
	}
	t.Fatalf("puffed: %v; want event 0 and then the colour reset to none", puffed)
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
