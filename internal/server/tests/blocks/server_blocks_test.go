package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestBreakingABlockReachesEveryone checks that a player breaking a block
// broadcasts the change to the other players, acknowledges it to the player who
// did it, and persists in the world.
func TestBreakingABlockReachesEveryone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The top of the stone at spawn is y=63: GroundLevel is the first air
	// block above it, so the stone runs from worldMinY to GroundLevel-1.
	const x, y, z = int32(0), int32(63), int32(0)

	// The block is stone before anything happens.
	if got := srv.BlockAt(x, y, z); got != 1 {
		t.Fatalf("block before break = %d, want 1 (stone)", got)
	}

	// Breaking it removes it from the world.
	actor.BreakBlock(x, y, z)

	// The other player sees the block become air.
	got := watcher.ExpectBlockUpdate()
	if got.X != x || got.Y != y || got.Z != z {
		t.Errorf("block update at (%d, %d, %d), want (%d, %d, %d)", got.X, got.Y, got.Z, x, y, z)
	}
	if got.State != 0 {
		t.Errorf("block state after break = %d, want 0 (air)", got.State)
	}

	// The player who did it is told the server has processed the change.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}

	// The change is in the world, not only on the wire.
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("block after break = %d, want 0 (air)", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingABlockReachesEveryone checks that a player placing a block from
// their hotbar broadcasts the change to the other players, acknowledges it, and
// puts the block in the world. The block is placed away from any player, since
// placing into one is rejected (see TestPlacingIntoAPlayerIsRejected).
func TestPlacingABlockReachesEveryone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a stone in the actor's main hand, the way a creative client does.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	// The equipment change telling the other players what the actor is
	// holding arrives before any block they place with it.
	watcher.ExpectEquipment(entityID)

	// Place a stone well away from any player: the actor is at spawn
	// (0.5, 64, 0.5), so a block at (5, 64, 0) does not overlap. It goes on
	// top of the stone at (5, 63, 0), against its top face.
	const clickX, clickY, clickZ = int32(5), int32(63), int32(0)
	const placeY = int32(64)

	// The block is air before anything happens.
	if got := srv.BlockAt(clickX, placeY, clickZ); got != 0 {
		t.Fatalf("block before place = %d, want 0 (air)", got)
	}

	actor.PlaceBlock(clickX, clickY, clickZ, protocol.FaceTop)

	// The other player sees the block become stone.
	got := watcher.ExpectBlockUpdate()
	if got.X != clickX || got.Y != placeY || got.Z != clickZ {
		t.Errorf("block update at (%d, %d, %d), want (%d, %d, %d)",
			got.X, got.Y, got.Z, clickX, placeY, clickZ)
	}
	if got.State != 1 {
		t.Errorf("block state after place = %d, want 1 (stone)", got.State)
	}

	// The player who did it is told the server has processed the change.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}

	// The change is in the world, not only on the wire.
	if got := srv.BlockAt(clickX, placeY, clickZ); got != 1 {
		t.Errorf("block after place = %d, want 1 (stone)", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingIntoAPlayerIsRejected checks that a block placed where a player is
// standing is refused: the world is not changed, the player who tried gets only
// an acknowledgement (which is enough for the modern client to revert its
// prediction), and the other players see nothing at all.
func TestPlacingIntoAPlayerIsRejected(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a stone in the actor's main hand.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)

	// The actor is standing at spawn (0.5, 64, 0.5). Placing a block at
	// (0, 64, 0) - on top of the stone at spawn - would put it where the
	// actor's body is, so the server refuses.
	const x, y, z = int32(0), int32(63), int32(0)
	const placeY = int32(64)

	actor.PlaceBlock(x, y, z, protocol.FaceTop)

	// The actor gets only an acknowledgement. The modern client captured the
	// block's old state before predicting the placement, and reverts to it
	// once the ack arrives - no Block Update is needed, the way the vanilla
	// server does not send one either.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}

	// The world did not change.
	if got := srv.BlockAt(x, placeY, z); got != 0 {
		t.Errorf("block after rejected place = %d, want 0 (air)", got)
	}

	// The watcher sees nothing: the rejection went only to the player who
	// tried. A ping flushes anything the server might have sent, and
	// expectPlay fails if a Block Update turns up instead of the answer.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingIntoAMobIsRejected checks the mobs' half of the placement test
// the player one pins: a block whose shape would land where an animal's body
// stands is refused the same vanilla Level.isUnobstructed way — the world and
// the other players see nothing, and the held stack is left unspent, survival
// being the mode that spends one at all. The same stack still places beside
// the chicken.
func TestPlacingIntoAMobIsRejected(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	// The loop is stopped so the chicken stands where the test put it for
	// as long as the placement's answer is in flight.
	srv.StopTick()

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	// Five stone in the hotbar: the stack a refused placement must leave
	// alone.
	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	// A chicken standing in the cell the placement is about to fill.
	srv.SpawnMob("minecraft:chicken", 3.5, 64, 0.5)
	actor.Settle()
	watcher.Settle()

	// A stone on the top face of the ground at (3, 63, 0) would fill the
	// chicken's own cell, (3, 64, 0), which the vanilla server refuses.
	// The tick is stopped, so its end never flushes the ack: the settle
	// proves the handler done and the flush puts the ack on the wire.
	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()

	// The actor gets an acknowledgement and the two echoes, which carry
	// the world's version of the clicked cell and the face's — the ground
	// as it was, the chicken's cell still air. The echoes are what tell
	// the client its predicted block to revert.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(3, 63, 0, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refused placement's echoes = (%d, %d), want (1, 0) — the ground and air",
			echoes[0], echoes[1])
	}

	// The world did not change, and neither did the stack.
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("block after rejected place = %d, want 0 (air)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 5 {
		t.Errorf("held stack after rejected place = %d of 5, want all 5 (unspent)", got.Count)
	}

	// The watcher sees nothing: the refusal went only to the player who
	// tried. A ping flushes anything the server might have sent, and
	// expectPlay fails if a Block Update turns up instead of the answer.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

	// The same stack still places where no body stands: the chicken was
	// the refusal, not the ground it played on. The ack is what says the
	// placement has been seen to before the world is asked about it.
	actor.PlaceBlock(2, 63, 2, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}

	if got := srv.BlockAt(2, 64, 2); got != 1 {
		t.Errorf("block after the free place = %d, want 1 (stone)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held stack after the free place = %d of 4, want one spent", got.Count)
	}

	// A fence into the same chicken is refused the same way: its tall
	// thin shape falls within the chicken's box, whatever parsing the
	// connected shape costs.
	fence, _ := registry.ItemID("minecraft:oak_fence")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 5})
	actor.Settle()
	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("fence ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(3, 63, 0, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refused fence's echoes = (%d, %d), want (1, 0) — the ground and air",
			echoes[0], echoes[1])
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("block after the refused fence = %d, want 0 (air)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 5 {
		t.Errorf("held stack after the refused fence = %d of 5, want all 5 (unspent)", got.Count)
	}

	// The fence places where the chicken misses its cell altogether —
	// its own cell beside, with no connection among the walls.
	actor.PlaceBlock(0, 63, 3, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("free fence ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(0, 64, 3); got != fenceState(t, nil) {
		t.Errorf("block after the free fence = %d, want the unconnected fence", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held stack after the free fence = %d of 4, want one spent", got.Count)
	}
}

// expectPlacementIntoBodyRefused places a stone into the cell (3, 64, 0) of
// a stopped survival server and demands the vanilla refusal: the ack, the
// two echoes of the unchanged cells, the air still in the cell and the
// stack unspent. It then places the same stack at a free cell and demands
// it lands, so a body standing in (3, 64, 0) is what refused the first.
func expectPlacementIntoBodyRefused(t *testing.T, srv *server.Server, actor *servertest.Client) {
	t.Helper()

	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(3, 63, 0, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refused placement's echoes = (%d, %d), want (1, 0) — the ground and air",
			echoes[0], echoes[1])
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("block after rejected place = %d, want 0 (air)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 5 {
		t.Errorf("held stack after rejected place = %d of 5, want all 5 (unspent)", got.Count)
	}

	actor.PlaceBlock(2, 63, 2, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("free place ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(2, 64, 2); got != 1 {
		t.Errorf("block after the free place = %d, want 1 (stone)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held stack after the free place = %d of 4, want one spent", got.Count)
	}
}

// TestPlacingIntoASpectatorIsAllowed checks that a spectator's body does not
// stop a placement: vanilla's isUnobstructed walks its entities through
// EntitySelector.NO_SPECTATORS, so a stone lands in a spectator's cell the
// way it lands around an item or an arrow, the spectator's blocksBuilding
// notwithstanding.
func TestPlacingIntoASpectatorIsAllowed(t *testing.T) {
	t.Parallel()
	// Ghost is an operator so its own mode can be switched by chat; the
	// placement hypotheses of spectator mode grant nothing else.
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost"))

	watcher := connect().Join("Watcher")

	// The body the block is placed into: a spectator standing at
	// (5.5, 64, 5.5), whose cell (5, 64, 5) the placement fills.
	ghost := connect().Join("Ghost")
	ghost.MoveTo(5.5, 64, 5.5, 0, 0)
	ghost.Settle()
	ghost.ChatCommand("gamemode spectator")
	ghost.ExpectModePackets(protocol.OfflineUUID("Ghost"), protocol.GameModeSpectator, true)

	// The placer stands close enough for the reach and clear of the
	// cell itself, at (4.5, 64, 6.5), whose box the target (5, 64, 5)
	// does not touch.
	actor := connect().Join("Placer")
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.Settle()
	// The joins and the moves are the watchers' business too; settled,
	// the only packets left to read are the placement's own.
	watcher.Settle()

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Placer", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})
	actor.Settle()

	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)

	// The placement went through: the watcher sees the stone arrive, the
	// world carries it, and the held stack is spent.
	got := watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 {
		t.Errorf("block update at (%d, %d, %d), want the spectator's cell (5, 64, 5)",
			got.X, got.Y, got.Z)
	}
	if got.State != 1 {
		t.Errorf("block state = %d, want 1 (stone)", got.State)
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(5, 64, 5); got != 1 {
		t.Errorf("world block after the place = %d, want 1 (stone)", got)
	}
	if got := srv.InventorySlot("Placer", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held stack after the place = %d of 5, want one spent", got.Count)
	}
}

// TestBlockChangesSurviveAChunkReload checks that a block a player broke stays
// broken after the chunk it is in is unloaded and generated again: the world
// keeps the change in memory, which is what makes it show up for a player who
// walks away and comes back.
func TestBlockChangesSurviveAChunkReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, c := servertest.Join(t, cfg, "Testificate")

	// Break a block at spawn, which is in the chunk (0, 0) the player starts
	// in.
	const x, y, z = int32(0), int32(63), int32(0)
	c.BreakBlock(x, y, z)
	c.ExpectBlockChange()

	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Fatalf("block after break = %d, want 0", got)
	}

	// Walk far enough away that the chunk at spawn unloads, and then come
	// back. The chunk is regenerated, but the block change was kept in the
	// chunk the world held on to, so it should still be air.
	// The view distance is 4, so walking 5 chunks east (to x=80) puts the
	// chunk at (0, 0) outside the view.
	c.MoveTo(80.5, 64, 0.5, 0, 0)
	c.Settle()

	// Coming back should regenerate the chunk with the change still in it.
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	c.Settle()

	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("block after reload = %d, want 0 (change did not persist)", got)
	}
}

// TestPlacingOrientedBlocks checks that the server picks the right block state
// for a placement: a log placed on the ground stands up, one placed against a
// wall lies flat; a furnace faces the player; a slab on the ground is the bottom
// half, one placed against a wall high is the top half; stairs face the way the
// player looks and follow the cursor for their half.
func TestPlacingOrientedBlocks(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	state := func(name string, props map[string]string) uint16 {
		return servertest.MustState(t, name, props)
	}
	var (
		logAxisY = state("minecraft:oak_log", map[string]string{"axis": "y"}) // placed on ground
		logAxisZ = state("minecraft:oak_log", map[string]string{"axis": "z"}) // placed against N/S wall
		logAxisX = state("minecraft:oak_log", map[string]string{"axis": "x"}) // placed against E/W wall

		furnaceNorth = state("minecraft:furnace", map[string]string{"facing": "north", "lit": "false"}) // player faces south
		furnaceSouth = state("minecraft:furnace", map[string]string{"facing": "south", "lit": "false"}) // player faces north

		slabBottom = state("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "false"}) // on ground
		slabTop    = state("minecraft:oak_slab", map[string]string{"type": "top", "waterlogged": "false"})    // against wall, high cursor

		stairsNorthBottom = state("minecraft:oak_stairs", map[string]string{
			"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": "false",
		})
		stairsSouthTop = state("minecraft:oak_stairs", map[string]string{
			"facing": "south", "half": "top", "shape": "straight", "waterlogged": "false",
		})
	)

	// The actor stands at (4.5, 64, 6.5): within the block interaction's
	// reach of every block this test clicks — vanilla refuses a click past
	// the reach — and clear of every cell the test builds, which used to be
	// arranged around the spawn the old facePlayer put the actor back on.
	// The stone ground is at y=63; placing on its top face puts a block at y=64.
	// Blocks are placed along z=5 to keep them in the same chunk.
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.Settle()

	// --- Logs: axis from the clicked face ---
	oakLog, _ := registry.ItemID("minecraft:oak_log")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: oakLog, Count: 1})
	watcher.ExpectEquipment(entityID)

	// On top of the stone (face=top) → axis=y, at (0, 64, 5).
	actor.PlaceBlock(0, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate().State; got != logAxisY {
		t.Errorf("log on ground: state=%d, want %d (axis=y)", got, logAxisY)
	}

	// Against the north face of that log → axis=z, at (0, 64, 4).
	actor.PlaceBlock(0, 64, 5, protocol.FaceNorth)
	if got := watcher.ExpectBlockUpdate().State; got != logAxisZ {
		t.Errorf("log against N face: state=%d, want %d (axis=z)", got, logAxisZ)
	}

	// Against the east face of the first log → axis=x, at (1, 64, 5).
	actor.PlaceBlock(0, 64, 5, protocol.FaceEast)
	if got := watcher.ExpectBlockUpdate().State; got != logAxisX {
		t.Errorf("log against E face: state=%d, want %d (axis=x)", got, logAxisX)
	}

	// --- Furnace: facing from the player's yaw (opposite) ---
	furnace, _ := registry.ItemID("minecraft:furnace")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: furnace, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Player faces south (yaw=0) → furnace faces north, at (3, 64, 5).
	actor.FacePlayer(4.5, 64, 6.5, 0, 0)
	actor.PlaceBlock(3, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate().State; got != furnaceNorth {
		t.Errorf("furnace facing south player: state=%d, want %d (facing=north)", got, furnaceNorth)
	}

	// Player faces north (yaw=180) → furnace faces south, at (4, 64, 5).
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.PlaceBlock(4, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate().State; got != furnaceSouth {
		t.Errorf("furnace facing north player: state=%d, want %d (facing=south)", got, furnaceSouth)
	}

	// --- Slab: type from cursor Y and face ---
	slab, _ := registry.ItemID("minecraft:oak_slab")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: slab, Count: 1})
	watcher.ExpectEquipment(entityID)

	// On top of the stone (face=top) → type=bottom, at (5, 64, 5).
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate().State; got != slabBottom {
		t.Errorf("slab on ground: state=%d, want %d (type=bottom)", got, slabBottom)
	}

	// Against the north face of the furnace at (3, 64, 5), cursor high →
	// type=top, at (3, 64, 4). A furnace opens its window when clicked, so
	// the actor sneaks to build against it, and stays crouched for the
	// placements against it further down.
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	watcher.ExpectMetadata()
	actor.PlaceBlockAt(3, 64, 5, protocol.FaceNorth, 0.8)
	if got := watcher.ExpectBlockUpdate().State; got != slabTop {
		t.Errorf("slab against wall, high cursor: state=%d, want %d (type=top)", got, slabTop)
	}

	// --- Stairs: facing from the look direction, half from face and cursor ---
	stairs, _ := registry.ItemID("minecraft:oak_stairs")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stairs, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Player faces north (yaw=180) → the stairs face north as well; on
	// ground → half=bottom, at (7, 64, 5).
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.PlaceBlock(7, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate().State; got != stairsNorthBottom {
		t.Errorf("stairs placed looking north: state=%d, want %d", got, stairsNorthBottom)
	}

	// Player faces south (yaw=0) → the stairs face south; against the north
	// face of the furnace at (4, 64, 5), cursor high → half=top, at (4, 64, 4).
	actor.FacePlayer(4.5, 64, 6.5, 0, 0)
	actor.PlaceBlockAt(4, 64, 5, protocol.FaceNorth, 0.8)
	if got := watcher.ExpectBlockUpdate().State; got != stairsSouthTop {
		t.Errorf("stairs placed looking south: state=%d, want %d", got, stairsSouthTop)
	}

	// Verify the states ended up in the world too.
	if got := srv.BlockAt(0, 64, 5); got != logAxisY {
		t.Errorf("world log at (0,64,5) = %d, want %d", got, logAxisY)
	}
	if got := srv.BlockAt(3, 64, 5); got != furnaceNorth {
		t.Errorf("world furnace at (3,64,5) = %d, want %d", got, furnaceNorth)
	}
	if got := srv.BlockAt(7, 64, 5); got != stairsNorthBottom {
		t.Errorf("world stairs at (7,64,5) = %d, want %d", got, stairsNorthBottom)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// trapdoorState is the block state ID an oak trapdoor carries with the given
// facing and half, shut, unpowered and dry, the defaults a placement leaves.
// It is a test helper for the expected state after a side placement.
func trapdoorState(t *testing.T, facing, half string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:oak_trapdoor", map[string]string{
		"facing": facing, "half": half,
	})
	if !ok {
		t.Fatalf("FindState oak_trapdoor facing=%s half=%s: not found", facing, half)
	}
	return s
}

// observerState returns the block state ID an observer would be in with the
// given facing, powered at its default, the way fenceState does for the
// fences.
func observerState(t *testing.T, facing string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:observer", map[string]string{"facing": facing})
	if !ok {
		t.Fatalf("FindState observer facing=%s: not found", facing)
	}
	return s
}

// pistonState returns the block state ID a piston would be in with the given
// facing, extended at its default.
func pistonState(t *testing.T, facing string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:piston", map[string]string{"facing": facing})
	if !ok {
		t.Fatalf("FindState piston facing=%s: not found", facing)
	}
	return s
}

// commandBlockState returns the block state ID a command block would be in with
// the given facing, conditional at its default, the way pistonState does for
// the pistons.
func commandBlockState(t *testing.T, name, facing string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, map[string]string{"facing": facing})
	if !ok {
		t.Fatalf("FindState %s facing=%s: not found", name, facing)
	}
	return s
}

// TestPlacingFencesConnects checks that placing two fences next to each other
// connects both: the second fence is placed with a connection toward the
// first, and the first fence is updated to connect back. The vanilla server's
// FenceBlock.updateShape does the same on placement and on the neighbour
// update that follows.
func TestPlacingFencesConnects(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Place a fence at (5, 64, 5): no fence neighbours, so all connections
	// are false (the default state).
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	fenceNone := fenceState(t, nil)
	if got := watcher.ExpectBlockUpdate(); got.State != fenceNone {
		t.Errorf("first fence state = %d, want %d (no connections)", got.State, fenceNone)
	}

	// Place a second fence at (6, 64, 5), east of the first. The new fence
	// connects west; the first fence is updated to connect east.
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	fenceWest := fenceState(t, map[string]string{"west": "true"})
	fenceEast := fenceState(t, map[string]string{"east": "true"})

	// The new fence's update arrives first (from the placement broadcast),
	// then the first fence's update (from the neighbour reconnection).
	got := watcher.ExpectBlockUpdate()
	if got.State != fenceWest {
		t.Errorf("second fence state = %d, want %d (west=true)", got.State, fenceWest)
	}
	if got.X != 6 || got.Y != 64 || got.Z != 5 {
		t.Errorf("second fence at (%d,%d,%d), want (6,64,5)", got.X, got.Y, got.Z)
	}
	got = watcher.ExpectBlockUpdate()
	if got.State != fenceEast {
		t.Errorf("first fence reconnected state = %d, want %d (east=true)", got.State, fenceEast)
	}
	if got.X != 5 || got.Y != 64 || got.Z != 5 {
		t.Errorf("first fence reconnect at (%d,%d,%d), want (5,64,5)", got.X, got.Y, got.Z)
	}

	// Both states ended up in the world.
	if got := srv.BlockAt(5, 64, 5); got != fenceEast {
		t.Errorf("world fence at (5,64,5) = %d, want %d", got, fenceEast)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceWest {
		t.Errorf("world fence at (6,64,5) = %d, want %d", got, fenceWest)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestBreakingAFenceUpdatesNeighbours checks that breaking a fence reconnects
// the fences left behind: a fence next to the broken one loses the connection
// that pointed at it.
func TestBreakingAFenceUpdatesNeighbours(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	// Place two connected fences at (5, 64, 5) and (6, 64, 5).
	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // new fence (west=true)
	watcher.ExpectBlockUpdate() // first fence reconnected (east=true)

	fenceNone := fenceState(t, nil)

	// Break the second fence. The first fence should lose its east connection.
	actor.BreakBlock(6, 64, 5)

	// The broken fence becomes air.
	got := watcher.ExpectBlockUpdate()
	if got.X != 6 || got.Y != 64 || got.Z != 5 || got.State != 0 {
		t.Errorf("break update = (%d,%d,%d) state %d, want (6,64,5) air", got.X, got.Y, got.Z, got.State)
	}
	// The first fence is reconnected to all-false.
	got = watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 {
		t.Errorf("reconnect update at (%d,%d,%d), want (5,64,5)", got.X, got.Y, got.Z)
	}
	if got.State != fenceNone {
		t.Errorf("reconnected fence state = %d, want %d (no connections)", got.State, fenceNone)
	}
	if got := srv.BlockAt(5, 64, 5); got != fenceNone {
		t.Errorf("world fence after break = %d, want %d", got, fenceNone)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// stairsState returns the block state ID of minecraft:oak_stairs with the
// given property overrides and the block's defaults elsewhere.
func stairsState(t *testing.T, overrides map[string]string) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:oak_stairs", overrides)
	if !ok {
		t.Fatalf("no oak_stairs state for %v", overrides)
	}
	return s
}

// TestPlacingStairsConnects checks that a stair placed beside an existing one
// takes its corner shape from it and gives the existing one its own corner
// shape in turn, on placement and when a neighbour appears or breaks, each
// reshape reaching the other players as a block update. A stair of the other
// half is no corner.
func TestPlacingStairsConnects(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The actor stands where every click is within the block interaction's
	// reach — vanilla refuses a click past the reach — and the stone ground
	// is at y=63, so placements on its top face land at y=64.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	stairs, _ := registry.ItemID("minecraft:oak_stairs")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stairs, Count: 1})
	watcher.ExpectEquipment(entityID)

	// A stair at (5, 64, 5): the actor looks north, and a stair faces the
	// look direction.
	actor.FacePlayer(4.5, 64, 4.5, 180, 0)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	got := watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 || got.State != stairsState(t, map[string]string{"facing": "north", "shape": "straight"}) {
		t.Fatalf("first stair = (%d,%d,%d) state %d, want (5,64,5) north straight", got.X, got.Y, got.Z, got.State)
	}

	// A stair to its north, the actor looking east: the new stair is
	// straight, and the first takes the outer corner — the second faces
	// its clockwise side — and the reshape is broadcast.
	actor.FacePlayer(4.5, 64, 4.5, 270, 0)
	actor.PlaceBlock(5, 63, 4, protocol.FaceTop)
	got = watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 4 || got.State != stairsState(t, map[string]string{"facing": "east", "shape": "straight"}) {
		t.Errorf("second stair = (%d,%d,%d) state %d, want (5,64,4) east straight", got.X, got.Y, got.Z, got.State)
	}
	got = watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 || got.State != stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}) {
		t.Errorf("first stair reshape = (%d,%d,%d) state %d, want (5,64,5) north outer_right", got.X, got.Y, got.Z, got.State)
	}

	// Breaking the second stair takes the corner away again.
	actor.BreakBlock(5, 64, 4)
	got = watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 4 || got.State != 0 {
		t.Errorf("break update = (%d,%d,%d) state %d, want (5,64,4) air", got.X, got.Y, got.Z, got.State)
	}
	got = watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 || got.State != stairsState(t, map[string]string{"facing": "north", "shape": "straight"}) {
		t.Errorf("first stair after break = (%d,%d,%d) state %d, want (5,64,5) north straight", got.X, got.Y, got.Z, got.State)
	}

	// A stair of the other half is no corner: place a bottom-half stair at
	// (7, 64, 5) and a top-half stair beside it at (7, 64, 4), the top half
	// coming from a cursor above the middle of the clicked face.
	actor.FacePlayer(4.5, 64, 4.5, 180, 0)
	actor.PlaceBlock(7, 63, 5, protocol.FaceTop)
	got = watcher.ExpectBlockUpdate()
	if got.X != 7 || got.Y != 64 || got.Z != 5 || got.State != stairsState(t, map[string]string{"facing": "north", "shape": "straight"}) {
		t.Errorf("half test stair = (%d,%d,%d) state %d, want (7,64,5) north straight", got.X, got.Y, got.Z, got.State)
	}
	actor.FacePlayer(4.5, 64, 4.5, 270, 0)
	actor.PlaceBlockAt(7, 64, 5, protocol.FaceNorth, 0.8)
	got = watcher.ExpectBlockUpdate()
	if got.X != 7 || got.Y != 64 || got.Z != 4 || got.State != stairsState(t, map[string]string{"facing": "east", "half": "top", "shape": "straight"}) {
		t.Errorf("top stair = (%d,%d,%d) state %d, want (7,64,4) east top straight", got.X, got.Y, got.Z, got.State)
	}
	// No reshape follows: the halves differ. The world keeps the first
	// stair straight, and no update for it comes out.
	if got := srv.BlockAt(7, 64, 5); got != stairsState(t, map[string]string{"facing": "north", "shape": "straight"}) {
		t.Errorf("stair beside a top-half stair = %d, want north straight", got)
	}

	actor.BreakBlock(7, 64, 4)
	got = watcher.ExpectBlockUpdate()
	if got.X != 7 || got.Y != 64 || got.Z != 4 || got.State != 0 {
		t.Errorf("top break update = (%d,%d,%d) state %d, want (7,64,4) air", got.X, got.Y, got.Z, got.State)
	}
	// A bottom-half stair in its place forms the corner.
	actor.PlaceBlock(7, 63, 4, protocol.FaceTop)
	got = watcher.ExpectBlockUpdate()
	if got.X != 7 || got.Y != 64 || got.Z != 4 || got.State != stairsState(t, map[string]string{"facing": "east", "shape": "straight"}) {
		t.Errorf("second bottom stair = (%d,%d,%d) state %d, want (7,64,4) east straight", got.X, got.Y, got.Z, got.State)
	}
	got = watcher.ExpectBlockUpdate()
	if got.X != 7 || got.Y != 64 || got.Z != 5 || got.State != stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}) {
		t.Errorf("half test reshape = (%d,%d,%d) state %d, want (7,64,5) north outer_right", got.X, got.Y, got.Z, got.State)
	}

	// The corner is in the world, not only on the wire.
	if got := srv.BlockAt(7, 64, 5); got != stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}) {
		t.Errorf("world stair = %d, want north outer_right", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingAStairInWaterWaterlogsIt checks that a stair placed into a water
// source takes waterlogged=true, the fluid the vanilla StairBlock reads at the
// placement's own cell.
// vanilla: StairBlock.getStateForPlacement
func TestPlacingAStairInWaterWaterlogsIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	// A water source in the cell the stair will fill.
	water, ok := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	if !ok || !srv.SetBlock(5, 64, 5, water) {
		t.Fatal("could not stand the water")
	}

	stairs, _ := registry.ItemID("minecraft:oak_stairs")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stairs, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Clicking the stone under the water places into the water's own cell.
	actor.FacePlayer(4.5, 64, 4.5, 180, 0)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)

	want := stairsState(t, map[string]string{"facing": "north", "waterlogged": "true"})
	got := watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 || got.State != want {
		t.Fatalf("stair in water = (%d,%d,%d) state %d, want (5,64,5) north waterlogged=true", got.X, got.Y, got.Z, got.State)
	}
	if srv.BlockAt(5, 64, 5) != want {
		t.Errorf("world stair in water = %d, want %d", srv.BlockAt(5, 64, 5), want)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingBarsConnects checks that placing two iron bars next to each other
// connects both, the way IronBarsBlock.attachsTo does on placement and on the
// neighbour update that follows.
func TestPlacingBarsConnects(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	bars, _ := registry.ItemID("minecraft:iron_bars")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: bars, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Place iron bars at (5, 64, 5): no bars neighbours, all connections false.
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	barsNone := barsState(t, nil)
	if got := watcher.ExpectBlockUpdate(); got.State != barsNone {
		t.Errorf("first bars state = %d, want %d (no connections)", got.State, barsNone)
	}

	// Place a second iron bars at (6, 64, 5), east of the first. The new bars
	// connects west; the first bars is updated to connect east.
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	barsWest := barsState(t, map[string]string{"west": "true"})
	barsEast := barsState(t, map[string]string{"east": "true"})

	got := watcher.ExpectBlockUpdate()
	if got.State != barsWest {
		t.Errorf("second bars state = %d, want %d (west=true)", got.State, barsWest)
	}
	got = watcher.ExpectBlockUpdate()
	if got.State != barsEast {
		t.Errorf("first bars reconnected state = %d, want %d (east=true)", got.State, barsEast)
	}

	if got := srv.BlockAt(5, 64, 5); got != barsEast {
		t.Errorf("world bars at (5,64,5) = %d, want %d", got, barsEast)
	}
	if got := srv.BlockAt(6, 64, 5); got != barsWest {
		t.Errorf("world bars at (6,64,5) = %d, want %d", got, barsWest)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingWallsConnects checks that placing two walls next to each other
// connects both, the way WallBlock.connectsTo does on placement and on the
// neighbour update that follows.
func TestPlacingWallsConnects(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	wall, _ := registry.ItemID("minecraft:cobblestone_wall")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wall, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Place a wall at (5, 64, 5): no wall neighbours. With no connections and
	// no block above, the post is up (corner: all sides none) and all sides
	// are none.
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	wallPost := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	})
	if got := watcher.ExpectBlockUpdate(); got.State != wallPost {
		t.Errorf("first wall state = %d, want %d (post up, no connections)", got.State, wallPost)
	}

	// Place a second wall at (6, 64, 5), east of the first. Both walls
	// connect to each other. With no block above, the arms are low. The
	// second wall has only west connected (a corner: east≠west), so its
	// post is up. The first wall has only east connected (also a corner),
	// so its post is also up.
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	wallWestLow := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	})
	wallEastLow := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "none",
	})

	got := watcher.ExpectBlockUpdate()
	if got.State != wallWestLow {
		t.Errorf("second wall state = %d, want %d (west=low)", got.State, wallWestLow)
	}
	got = watcher.ExpectBlockUpdate()
	if got.State != wallEastLow {
		t.Errorf("first wall reconnected state = %d, want %d (east=low)", got.State, wallEastLow)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// the hotbar is empty puts a fresh stack in the selected slot - the first
// empty slot is the selected one - and that the other players see the new
// held item.
func TestPickBlockFillsEmptySelectedSlot(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The block at the actor's feet is stone, the flat-world spawn.
	stone, _ := registry.ItemID("minecraft:stone")
	if state := registry.BlockStates["minecraft:stone"]; state == 0 {
		t.Fatalf("stone block state is 0; the registry did not load")
	}

	// Middle-click the stone. The actor's hotbar is empty, so the selected
	// slot (index 0) is the first empty one and is filled in place; the
	// selection does not move.
	actor.PickBlock(0, 63, 0)

	// The actor hears Set Held Item for the slot it already had - vanilla
	// sends it on every accepted pick - then that the selected slot now
	// holds a stone.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 0 {
		t.Errorf("held item = %d, want 0 (the selection stays put)", got)
	}
	slotData := actor.ExpectPlay(protocol.IDPlayContainerSlot)
	r := protocol.NewReader(slotData)
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("window ID = %d, want 0 (the player's inventory)", got)
	}
	r.ReadVarInt() // state ID
	if got := r.ReadI16(); got != player.HotbarSlot {
		t.Errorf("slot = %d, want %d (the selected hotbar slot)", got, player.HotbarSlot)
	}
	stack, _ := r.ReadSlot()
	if stack.ItemID != stone || stack.Count != 1 {
		t.Errorf("slot = %+v, want a single stone", stack)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set container slot: %v", err)
	}

	// The other players see the actor now holding a stone.
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand = %+v, want a single stone", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockSurvivalSwapsTheHeldSlotForTheInventoryItem checks that
// middle-clicking a block whose item a survival player keeps in the main
// inventory swaps that stack into the slot the selection moved to: the
// stone lands in the hand, and the diamond stays where it was.
func TestPickBlockSurvivalSwapsTheHeldSlotForTheInventoryItem(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// A diamond held in the selected slot, and the stone in the main
	// inventory.
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	srv.GiveSlot("Actor", player.MainSlot, protocol.ItemStack{ItemID: stone, Count: 32})

	// Middle-click the stone at the actor's feet. The stone is carried
	// outside the hotbar, so it is swapped into the hand.
	actor.PickBlock(0, 63, 0)

	// The selection moves to the empty slot the pick chose, and the two
	// slots the swap changed are told apart, in either order.
	sawHeld, sawHotbar, sawMain := false, false, false
	for gotID, data := range actor.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayUpdateTime,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity:
			// A time sync, or a nearby body moving, that fell between the
			// pick block and this read; skip it.
		case protocol.IDPlayHeldItem:
			if got := protocol.NewReader(data).ReadVarInt(); got != 1 {
				t.Errorf("held item = %d, want 1 (the empty slot the pick chose)", got)
			}
			sawHeld = true
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 0 {
				t.Errorf("window ID = %d, want 0 (the player's inventory)", got)
			}
			r.ReadVarInt() // state ID
			slot := r.ReadI16()
			stack, _ := r.ReadSlot()
			switch slot {
			case player.HotbarSlot + 1:
				if stack.ItemID != stone || stack.Count != 32 {
					t.Errorf("held slot = %+v, want the stone from the main inventory", stack)
				}
				sawHotbar = true
			case player.MainSlot:
				if !stack.Empty() {
					t.Errorf("main slot = %+v, want it emptied by the swap", stack)
				}
				sawMain = true
			default:
				t.Errorf("container slot = %d, want %d or %d", slot, player.HotbarSlot+1, player.MainSlot)
			}
			if err := r.Err(); err != nil {
				t.Fatalf("set container slot: %v", err)
			}
		default:
			t.Fatalf("unexpected packet 0x%02X after pick block", gotID)
		}
		if sawHeld && sawHotbar && sawMain {
			break
		}
	}
	if !sawHeld || !sawHotbar || !sawMain {
		t.Fatalf("pick block response incomplete within %s (held %v, hotbar %v, main %v)", servertest.ExpectTimeout, sawHeld, sawHotbar, sawMain)
	}

	// Nothing followed the swap.
	actor.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	actor.ExpectPlay(protocol.IDPlayPingResponse)

	// The swap is real: the server's slots say so, and the other players
	// see the stone in the actor's hand.
	if got := srv.InventorySlot("Actor", player.HotbarSlot+1); got.ItemID != stone || got.Count != 32 {
		t.Errorf("hotbar slot 1 = %+v, want the stone", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != diamond || got.Count != 1 {
		t.Errorf("hotbar slot 0 = %+v, want the diamond it kept", got)
	}
	if got := srv.InventorySlot("Actor", player.MainSlot); !got.Empty() {
		t.Errorf("main slot = %+v, want it emptied by the swap", got)
	}
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 32 {
		t.Errorf("main hand = %+v, want the stone from the main inventory", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingABlockReplacesShortGrass checks that a block placed by clicking
// on short_grass overwrites it at the same position, the way the vanilla
// server's BlockPlaceContext uses the clicked position when canBeReplaced
// returns true. The stone goes where the grass was, not in the air above it.
func TestPlacingABlockReplacesShortGrass(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	// Place dirt at (5, 64, 5) on the stone, then short_grass on the dirt
	// (short grass refuses stone).
	dirt, _ := registry.ItemID("minecraft:dirt")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: dirt, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)

	dirtState := registry.BlockStates["minecraft:dirt"]
	if got := watcher.ExpectBlockUpdate(); got.State != dirtState {
		t.Errorf("dirt state = %d, want %d", got.State, dirtState)
	}
	if got := actor.ExpectBlockChange(); got.State != dirtState {
		t.Errorf("actor dirt state = %d, want %d", got.State, dirtState)
	}

	grass, _ := registry.ItemID("minecraft:short_grass")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: grass, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 64, 5, protocol.FaceTop)

	grassState := registry.BlockStates["minecraft:short_grass"]
	if got := watcher.ExpectBlockUpdate(); got.State != grassState {
		t.Errorf("grass state = %d, want %d", got.State, grassState)
	}
	if got := actor.ExpectBlockChange(); got.State != grassState {
		t.Errorf("actor grass state = %d, want %d", got.State, grassState)
	}

	// Switch to stone and click on the grass itself. The grass is
	// replaceable, so the stone replaces it at the same position (5, 65, 5),
	// rather than going in the air above it (5, 66, 5).
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 65, 5, protocol.FaceTop)

	// The watcher sees the block at (5, 65, 5) become stone.
	got := watcher.ExpectBlockUpdate()
	if got.State != 1 {
		t.Errorf("stone state = %d, want 1 (stone)", got.State)
	}
	if got.X != 5 || got.Y != 65 || got.Z != 5 {
		t.Errorf("stone placed at (%d,%d,%d), want (5,65,5) (replacing grass)", got.X, got.Y, got.Z)
	}
	if got := actor.ExpectBlockChange(); got.State != 1 {
		t.Errorf("actor stone state = %d, want 1 (stone)", got.State)
	}

	// The world has stone where the grass was, and air above it.
	if got := srv.BlockAt(5, 65, 5); got != 1 {
		t.Errorf("world block at (5,65,5) = %d, want 1 (stone replaced grass)", got)
	}
	if got := srv.BlockAt(5, 66, 5); got != 0 {
		t.Errorf("world block at (5,66,5) = %d, want 0 (stone did not go above)", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingABlockReplacesWater checks that a block placed by clicking on
// water overwrites it at the same position: water is in the
// minecraft:replaceable tag. Since water has no item form, the test sets it
// directly in the world before the placement.
func TestPlacingABlockReplacesWater(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	// Put water at (5, 64, 5) directly, the way a world generator would.
	// Water's default state is level=0, the first in AllStates.
	waterState := registry.AllStates["minecraft:water"][0].ID
	srv.SetBlock(5, 64, 5, waterState)
	if got := srv.BlockAt(5, 64, 5); got != waterState {
		t.Fatalf("setup: water = %d, want %d", got, waterState)
	}

	// Click on the water itself. The water is replaceable, so the stone
	// replaces it at the same position (5, 64, 5).
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 64, 5, protocol.FaceTop)

	// The watcher sees the block at (5, 64, 5) become stone.
	got := watcher.ExpectBlockUpdate()
	if got.State != 1 {
		t.Errorf("stone state = %d, want 1 (stone)", got.State)
	}
	if got.X != 5 || got.Y != 64 || got.Z != 5 {
		t.Errorf("stone placed at (%d,%d,%d), want (5,64,5) (replacing water)", got.X, got.Y, got.Z)
	}
	if got := actor.ExpectBlockChange(); got.State != 1 {
		t.Errorf("actor stone state = %d, want 1 (stone)", got.State)
	}

	// The world has stone where the water was.
	if got := srv.BlockAt(5, 64, 5); got != 1 {
		t.Errorf("world block = %d, want 1 (stone replaced water)", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingABlockDoesNotReplaceStone checks that a block placed where another
// block already stands is rejected when that block is not replaceable: stone is
// a ordinary solid block, so a second stone placed on top of the first is
// refused. This was the behaviour before replaceable blocks were modelled, and
// it still holds for every non-replaceable block.
func TestPlacingABlockDoesNotReplaceStone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	// Place a stone at (5, 64, 5) on top of the stone at (5, 63, 5).
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got.State != 1 {
		t.Errorf("first stone state = %d, want 1", got.State)
	}
	actor.ExpectBlockChange()

	// Try to place a second stone at the same position. The target (5, 64, 5)
	// already has stone, which is NOT replaceable, so the placement is rejected.
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)

	// The actor gets only an ack.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("rejected stone ack = %d, want 1", seq)
	}

	// The world still has exactly one stone at (5, 64, 5).
	if got := srv.BlockAt(5, 64, 5); got != 1 {
		t.Errorf("world block = %d, want 1 (stone, not replaced)", got)
	}

	// The watcher sees nothing for the rejected placement.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestADigBeyondReachBreaksNothing digs at the block whose box's near face
// lies five and a half blocks from the digger's eye — the reach the block
// interaction range and its verification buffer allow, the distance the
// strict boundary refuses — and holds the dig refused: the block stays, the
// acknowledgement the client's prediction waits for still goes out; the
// packet is acked before the reach is verified. A block a step nearer takes
// the dig.
// The distances are measured eye to the block's own box: the digger's feet a
// block and three fifths under its eyes, the blocks stood one over the
// ground so the eye is level with them.
// vanilla: ServerGamePacketListenerImpl.handleBlockBreakAction
func TestADigBeyondReachBreaksNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.CombatTestConfig() // survival, the mode a dig costs something in
	srv, actor, _ := servertest.JoinAs(t, cfg, "Digger")
	actor.MoveToGround(2.5, 64, 5.5, true)
	actor.Settle()
	// An efficiency V pickaxe's share of the stone is over the seven
	// tenths (34 / 1.5 / 30) the finished click breaks at, so the dig's
	// break lands on its click and the dig's pace stays out of what the
	// reach test reads.
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 5))
	// A tick puts Efficiency's mining_efficiency on the player.
	servertest.RunTicks(t, srv, 1)

	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(8, 65, 5, stone) // the box's near face 5.5 from the eye
	srv.SetBlock(7, 65, 5, stone) // the box's near face 4.5 from the eye

	// The far block: the dig refused, the ack sent all the same.
	actor.BreakBlock(8, 65, 5)
	if got := actor.ExpectBlockAck(); got != 1 {
		actor.T.Errorf("the refused dig acked sequence %d, want 1", got)
	}
	if got := srv.BlockAt(8, 65, 5); got != stone {
		actor.T.Errorf("the block past the reach broke, want it standing")
	}

	// The near block: the dig taken.
	actor.BreakBlock(7, 65, 5)
	if got := actor.ExpectBlockAck(); got != 1 {
		actor.T.Errorf("the taken dig acked sequence %d, want 1", got)
	}
	if got := srv.BlockAt(7, 65, 5); got != 0 {
		actor.T.Errorf("the block within the reach stands, want it broken")
	}
}

// TestAPlacementBeyondReachPlacesNothing sets its builder clicking at the
// block whose box's near face lies five and a half blocks from the eye, and
// holds the placement refused: nothing built, but the acknowledgement the
// prediction waits for still sent, the sequence being acked before the reach
// is verified. A click a block nearer builds.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestAPlacementBeyondReachPlacesNothing(t *testing.T) {
	t.Parallel()
	srv, builder, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Builder")
	builder.MoveToGround(2.5, 64, 5.5, true)
	builder.Settle()

	stone := servertest.BlockState(t, "minecraft:stone")
	dirt := servertest.MustItemID(t, "minecraft:dirt")
	srv.SetBlock(8, 65, 5, stone) // the box's near face 5.5 from the eye
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: dirt, Count: 1})

	// The far click: the dirt stays in the hand and nothing is built.
	builder.PlaceBlock(8, 65, 5, protocol.FaceTop)
	if got := builder.ExpectBlockAck(); got != 1 {
		builder.T.Errorf("the refused placement acked sequence %d, want 1", got)
	}
	if got := srv.BlockAt(8, 66, 5); got != 0 {
		builder.T.Errorf("a block was built past the reach, want nothing placed")
	}

	// The near click: the dirt goes onto the ground.
	builder.PlaceBlock(7, 65, 5, protocol.FaceTop)
	if got := builder.ExpectBlockAck(); got != 1 {
		builder.T.Errorf("the taken placement acked sequence %d, want 1", got)
	}
	if got := srv.BlockAt(7, 65, 5); got == 0 {
		builder.T.Errorf("nothing was built within the reach, want the dirt placed")
	}
}
