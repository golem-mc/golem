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

// TestPlacingIntoAPrimedTNTIsRejected checks the primed TNT's half of the
// placement test: vanilla's PrimedTnt carries blocksBuilding, so a block
// whose shape would land in its box is refused, the same way a mob's body
// refuses one.
func TestPlacingIntoAPrimedTNTIsRejected(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	actor := connect().Join("Actor")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	// A primed TNT at rest in the cell the placement is about to fill,
	// burning for the default eighty so the stopped loop never spends it.
	srv.SpawnTNT(3.5, 64, 0.5, 80)
	actor.Settle()

	expectPlacementIntoBodyRefused(t, srv, actor)
}

// TestAPrimedTNTInAnotherDimensionLeavesAPlacementAlone checks the
// dimension filter the obstruction walk runs: a TNT burning at the same
// coordinates in the nether is nowhere near the overworld block, so the
// placement lands.
func TestAPrimedTNTInAnotherDimensionLeavesAPlacementAlone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	actor := connect().Join("Actor")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	srv.SpawnTNTInDimension(world.NetherName, 3.5, 64, 0.5, 80)
	actor.Settle()

	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(3, 64, 0); got != 1 {
		t.Errorf("block after the nether TNT = %d, want 1 (stone)", got)
	}
}

// TestPlacingIntoAFallingBlockIsRejected checks the falling block's half:
// vanilla's FallingBlockEntity carries blocksBuilding, so a block placed
// into the cell it fills mid-fall is refused.
func TestPlacingIntoAFallingBlockIsRejected(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	actor := connect().Join("Actor")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	srv.SpawnFallingBlock(3, 64, 0, sand)
	actor.Settle()

	expectPlacementIntoBodyRefused(t, srv, actor)
}

// TestPlacingIntoABoatIsRejected checks the boat's half: vanilla's
// AbstractBoat carries blocksBuilding, so a block whose shape would land in
// the hull's box is refused.
func TestPlacingIntoABoatIsRejected(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	actor := connect().Join("Actor")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	srv.SpawnBoat(3.5, 64, 0.5, "minecraft:oak_boat")
	actor.Settle()

	expectPlacementIntoBodyRefused(t, srv, actor)
}

// TestPlacingIntoADyingMobIsRejected checks the corpse's half: vanilla
// keeps a dead body's box for the twenty ticks of its deathTime, and a
// placement into it is refused until they are up.
func TestPlacingIntoADyingMobIsRejected(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	actor := connect().Join("Actor")

	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 5})

	chicken := srv.SpawnMob("minecraft:chicken", 3.5, 64, 0.5)
	actor.Settle()

	if !srv.KillMob(chicken) {
		t.Fatal("the chicken was not there to kill")
	}
	// The death tick moves the body out of the herd and into the dying
	// list; the death event it tells the actor is read off before the
	// placement's own answer.
	srv.TickMobs(1)
	actor.Settle()

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

	// The corpse's twenty ticks run out and the cell takes the stone.
	srv.TickMobs(25)
	actor.Settle()
	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence after the corpse was gone = %d, want 1", seq)
	}
	if got := srv.BlockAt(3, 64, 0); got != 1 {
		t.Errorf("block after the corpse was gone = %d, want 1 (stone)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held stack after the corpse was gone = %d of 4, want one spent", got.Count)
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

// TestPlacingANonCollidableBlockIntoAPlayer checks that a block with no
// collision shape - a torch, a sign, a button - can be placed where a player
// is standing, the way the vanilla server allows: its placement test checks the
// block's shape against every entity, and an empty shape intersects nothing.
func TestPlacingANonCollidableBlockIntoAPlayer(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a torch in the actor's main hand.
	torchItem, _ := registry.ItemID("minecraft:torch")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: torchItem, Count: 1})
	watcher.ExpectEquipment(entityID)

	// The actor is standing at spawn (0.5, 64, 0.5). Placing a torch on top
	// of the stone at spawn - at (0, 64, 0) - would put it where the actor's
	// body is, but a torch has no collision shape, so the vanilla server
	// allows it and so do we.
	const x, y, z = int32(0), int32(63), int32(0)
	const placeY = int32(64)

	// The torch's default block state.
	torchState := registry.BlockStates["minecraft:torch"]

	actor.PlaceBlock(x, y, z, protocol.FaceTop)

	// Both players see the torch appear, because the placement succeeded.
	got := watcher.ExpectBlockUpdate()
	if got.X != x || got.Y != placeY || got.Z != z {
		t.Errorf("watcher block update at (%d, %d, %d), want (%d, %d, %d)",
			got.X, got.Y, got.Z, x, placeY, z)
	}
	if got.State != torchState {
		t.Errorf("watcher block state = %d, want %d (torch)", got.State, torchState)
	}

	// The actor gets the block update too (the broadcast goes to everyone),
	// and behind it the ack the tick end flushes.
	if got := actor.ExpectBlockChange(); got.State != torchState {
		t.Errorf("actor block state = %d, want %d (torch)", got.State, torchState)
	}

	// The torch is in the world.
	if got := srv.BlockAt(x, placeY, z); got != torchState {
		t.Errorf("world torch = %d, want %d", got, torchState)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
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

// TestPlacingTrapdoorsReadsTheClickedFace checks TrapDoorBlock.getStateForPlacement:
// a click on another block's side face stands the trapdoor on the face it was
// clicked against, its half from the cursor height alone, whichever way the
// player looks - the look decides the facing only for a top or bottom click,
// which faces the leaf back across the placer.
func TestPlacingTrapdoorsReadsTheClickedFace(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Three stone posts stand beside the actor, each with four free side
	// cells: one per cursor height a side leaf takes, the third for the
	// halfway boundary. Two more posts carry the top and bottom clicks that
	// read the look instead. The actor stands within the creative reach of
	// every one of them.
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, p := range [][3]int32{
		{6, 64, 8}, {10, 64, 8}, {12, 64, 8}, {5, 64, 12}, {11, 65, 12},
	} {
		srv.SetBlock(p[0], p[1], p[2], stone)
	}

	trapdoor, _ := registry.ItemID("minecraft:oak_trapdoor")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: trapdoor, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Looking west (yaw 90) and looking south (yaw 0) each differ from the
	// side face clicked and from the block facing the look would give, so a
	// facing that ignores the click lands on the wrong one of the four.
	cases := []struct {
		desc                   string
		clickX, clickY, clickZ int32
		face                   int32
		cursorY                float32
		yaw                    float32
		destX, destY, destZ    int32
		want                   uint16
	}{
		{"east face, cursor low", 6, 64, 8, protocol.FaceEast, 0.3, 0, 7, 64, 8, trapdoorState(t, "east", "bottom")},
		{"west face, cursor low", 6, 64, 8, protocol.FaceWest, 0.3, 0, 5, 64, 8, trapdoorState(t, "west", "bottom")},
		{"north face, cursor low", 6, 64, 8, protocol.FaceNorth, 0.3, 90, 6, 64, 7, trapdoorState(t, "north", "bottom")},
		{"south face, cursor low", 6, 64, 8, protocol.FaceSouth, 0.3, 90, 6, 64, 9, trapdoorState(t, "south", "bottom")},
		{"east face, cursor high", 10, 64, 8, protocol.FaceEast, 0.8, 0, 11, 64, 8, trapdoorState(t, "east", "top")},
		{"west face, cursor high", 10, 64, 8, protocol.FaceWest, 0.8, 0, 9, 64, 8, trapdoorState(t, "west", "top")},
		{"north face, cursor high", 10, 64, 8, protocol.FaceNorth, 0.8, 90, 10, 64, 7, trapdoorState(t, "north", "top")},
		{"south face, cursor high", 10, 64, 8, protocol.FaceSouth, 0.8, 90, 10, 64, 9, trapdoorState(t, "south", "top")},
		// A cursor exactly halfway stays the bottom half.
		{"east face, cursor halfway", 12, 64, 8, protocol.FaceEast, 0.5, 0, 13, 64, 8, trapdoorState(t, "east", "bottom")},
		// A top click reads the look, the leaf facing back across the actor:
		// looking west, it faces east, and stands in the bottom half.
		{"top face", 5, 64, 12, protocol.FaceTop, 0.8, 90, 5, 65, 12, trapdoorState(t, "east", "bottom")},
		// A bottom click faces the look the same way, in the top half.
		{"bottom face", 11, 65, 12, protocol.FaceBottom, 0.3, 90, 11, 64, 12, trapdoorState(t, "east", "top")},
	}
	for _, c := range cases {
		actor.FacePlayer(8.5, 64, 8.5, c.yaw, 0)
		actor.PlaceBlockAt(c.clickX, c.clickY, c.clickZ, c.face, c.cursorY)
		got := watcher.ExpectBlockUpdate()
		want := servertest.BlockChange{X: c.destX, Y: c.destY, Z: c.destZ, State: c.want}
		if got != want {
			t.Errorf("%s: %+v, want %+v", c.desc, got, want)
		}
		if worldState := srv.BlockAt(c.destX, c.destY, c.destZ); worldState != c.want {
			t.Errorf("%s: world block at (%d,%d,%d) = %d, want %d",
				c.desc, c.destX, c.destY, c.destZ, worldState, c.want)
		}
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingABigDripleafReadsTheBlockBelow checks BigDripleafBlock's
// placement and updateShape: a leaf placed on a big dripleaf or stem below
// copies that block's FACING rather than facing the player, and a leaf
// placed on a leaf turns the lower leaf into a stem keeping the facing and
// waterlogging. Stacking leaves this way is how a dripleaf is grown by
// hand.
func TestPlacingABigDripleafReadsTheBlockBelow(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Clay is in #supports_big_dripleaf, which a stem and a leaf both need
	// under them. The three posts stand within the actor's reach.
	srv.SetBlock(3, 63, 0, servertest.BlockState(t, "minecraft:clay"))
	srv.SetBlock(5, 63, 0, servertest.BlockState(t, "minecraft:clay"))
	srv.SetBlock(7, 63, 0, servertest.BlockState(t, "minecraft:clay"))
	srv.SetBlock(3, 64, 0, servertest.BigDripleafState(t, "minecraft:big_dripleaf_stem", "east", false))
	srv.SetBlock(5, 64, 0, servertest.BigDripleafState(t, "minecraft:big_dripleaf", "east", false))
	srv.SetBlock(7, 64, 0, servertest.BigDripleafState(t, "minecraft:big_dripleaf", "east", true))

	leaf, _ := registry.ItemID("minecraft:big_dripleaf")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: leaf, Count: 1})
	watcher.ExpectEquipment(entityID)

	wantLeafEast := servertest.BigDripleafState(t, "minecraft:big_dripleaf", "east", false)
	wantStemEast := servertest.BigDripleafState(t, "minecraft:big_dripleaf_stem", "east", false)

	// Looking south (yaw 0), a look-read facing would be north, so an east
	// leaf can only come from the block below. A leaf on the stem copies
	// the stem's facing.
	actor.FacePlayer(4.5, 64, -2.5, 0, 0)
	actor.PlaceBlock(3, 64, 0, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 3, Y: 65, Z: 0, State: wantLeafEast}) {
		t.Errorf("leaf on a stem: %+v, want %+v (facing=east)", got, servertest.BlockChange{X: 3, Y: 65, Z: 0, State: wantLeafEast})
	}
	if got := srv.BlockAt(3, 65, 0); got != wantLeafEast {
		t.Errorf("world leaf on a stem = %d, want %d (facing=east)", got, wantLeafEast)
	}

	// A leaf on a leaf copies the facing and turns the lower leaf into a
	// stem with it.
	actor.FacePlayer(4.5, 64, -2.5, 0, 0)
	actor.PlaceBlock(5, 64, 0, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 5, Y: 65, Z: 0, State: wantLeafEast}) {
		t.Errorf("upper leaf on a leaf: %+v, want %+v (facing=east)", got, servertest.BlockChange{X: 5, Y: 65, Z: 0, State: wantLeafEast})
	}
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 5, Y: 64, Z: 0, State: wantStemEast}) {
		t.Errorf("lower leaf turned stem: %+v, want %+v", got, servertest.BlockChange{X: 5, Y: 64, Z: 0, State: wantStemEast})
	}
	if got := srv.BlockAt(5, 65, 0); got != wantLeafEast {
		t.Errorf("world upper leaf = %d, want %d (facing=east)", got, wantLeafEast)
	}
	if got := srv.BlockAt(5, 64, 0); got != wantStemEast {
		t.Errorf("world lower stem = %d, want %d (facing=east)", got, wantStemEast)
	}

	// A waterlogged lower leaf keeps its waterlogging when it becomes a
	// stem; the dry leaf placed above it is unaffected.
	wantStemWet := servertest.BigDripleafState(t, "minecraft:big_dripleaf_stem", "east", true)
	actor.FacePlayer(4.5, 64, -2.5, 0, 0)
	actor.PlaceBlock(7, 64, 0, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 7, Y: 65, Z: 0, State: wantLeafEast}) {
		t.Errorf("upper leaf on a waterlogged leaf: %+v, want %+v (facing=east)", got, servertest.BlockChange{X: 7, Y: 65, Z: 0, State: wantLeafEast})
	}
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 7, Y: 64, Z: 0, State: wantStemWet}) {
		t.Errorf("waterlogged lower leaf turned stem: %+v, want %+v (waterlogged=true)", got, servertest.BlockChange{X: 7, Y: 64, Z: 0, State: wantStemWet})
	}
	if got := srv.BlockAt(7, 65, 0); got != wantLeafEast {
		t.Errorf("world upper leaf = %d, want %d (facing=east)", got, wantLeafEast)
	}
	if got := srv.BlockAt(7, 64, 0); got != wantStemWet {
		t.Errorf("world waterlogged stem = %d, want %d (waterlogged=true)", got, wantStemWet)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
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

// TestObserverAndPistonFaceThePitch checks that an observer and a piston
// placed while looking steeply down or up point vertically: the vanilla
// getStateForPlacement reads the pitch through the nearest looking direction,
// the observer watching the way the player looks and the piston the other
// way, its head toward the placer.
func TestObserverAndPistonFaceThePitch(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Where every block this test clicks lies within the block
	// interaction's reach of the actor: vanilla refuses a click past
	// the reach, so the actor stands where the clicks are. The stone
	// ground is at y=63; placing on its top face puts a block at y=64.
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	observer, _ := registry.ItemID("minecraft:observer")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: observer, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Looking steeply down, the nearest looking direction is down, so the
	// observer's watched face points down, into the ground.
	actor.FacePlayer(4.5, 64, 4.5, 0, 60)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 5, Y: 64, Z: 5, State: observerState(t, "down")}) {
		t.Errorf("observer placed looking down: %+v, want facing=down", got)
	}

	// Looking steeply up, it points up instead.
	actor.FacePlayer(4.5, 64, 4.5, 0, -60)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 6, Y: 64, Z: 5, State: observerState(t, "up")}) {
		t.Errorf("observer placed looking up: %+v, want facing=up", got)
	}

	piston, _ := registry.ItemID("minecraft:piston")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: piston, Count: 1})
	watcher.ExpectEquipment(entityID)

	// The piston takes the look reversed: looking down stands the head up,
	// toward the placer.
	actor.FacePlayer(4.5, 64, 4.5, 0, 60)
	actor.PlaceBlock(5, 63, 4, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 5, Y: 64, Z: 4, State: pistonState(t, "up")}) {
		t.Errorf("piston placed looking down: %+v, want facing=up", got)
	}

	// Looking up points the head down.
	actor.FacePlayer(4.5, 64, 4.5, 0, -60)
	actor.PlaceBlock(6, 63, 4, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got != (servertest.BlockChange{X: 6, Y: 64, Z: 4, State: pistonState(t, "down")}) {
		t.Errorf("piston placed looking up: %+v, want facing=down", got)
	}

	// The world carries the same facings.
	for _, c := range []struct {
		x, y, z int32
		want    uint16
	}{
		{5, 64, 5, observerState(t, "down")},
		{6, 64, 5, observerState(t, "up")},
		{5, 64, 4, pistonState(t, "up")},
		{6, 64, 4, pistonState(t, "down")},
	} {
		if got := srv.BlockAt(c.x, c.y, c.z); got != c.want {
			t.Errorf("world block at (%d,%d,%d) = %d, want %d", c.x, c.y, c.z, got, c.want)
		}
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
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

// TestCommandBlocksFaceTheLook checks all three command blocks against
// CommandBlock.getStateForPlacement: FACING is the nearest looking direction
// reversed, so the pitch counts - a block placed looking steeply down faces
// up, away from the placer - and a horizontal look faces back across the
// clicked cell.
func TestCommandBlocksFaceTheLook(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	// Laying a command block takes the gamemaster permission as well as
	// creative mode, so the actor is an operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Actor"))

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The actor stands where every clicked block is within reach; the stone
	// ground is at y=63, so a top-face click places at y=64.
	actor.MoveToGround(4.5, 64, 5.5, true)
	actor.Settle()

	cases := []struct {
		name   string
		item   string
		x      int32
		yaw    float32
		pitch  float32
		facing string
	}{
		{"command_block looking down", "minecraft:command_block", 5, 0, 60, "up"},
		{"command_block looking west", "minecraft:command_block", 6, 90, 0, "east"},
		{"chain_command_block looking west", "minecraft:chain_command_block", 7, 90, 0, "east"},
		{"repeating_command_block looking west", "minecraft:repeating_command_block", 8, 90, 0, "east"},
	}
	for _, c := range cases {
		item, _ := registry.ItemID(c.item)
		actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 1})
		watcher.ExpectEquipment(entityID)
		actor.FacePlayer(4.5, 64, 5.5, c.yaw, c.pitch)
		actor.PlaceBlock(c.x, 63, 5, protocol.FaceTop)
		want := servertest.BlockChange{X: c.x, Y: 64, Z: 5, State: commandBlockState(t, c.item, c.facing)}
		if got := watcher.ExpectBlockUpdate(); got != want {
			t.Errorf("%s: %+v, want %+v", c.name, got, want)
		}
		if got := srv.BlockAt(c.x, 64, 5); got != want.State {
			t.Errorf("%s: world state = %d, want %d", c.name, got, want.State)
		}
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
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

// TestFenceConnectsToSolidBlock checks that a fence connects to a solid block
// such as stone, not only to other fences. The vanilla server's
// FenceBlock.updateShape connects to any neighbour whose face is sturdy.
func TestFenceConnectsToSolidBlock(t *testing.T) {
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

	// Place a stone at (5, 64, 5), then a fence at (6, 64, 5) next to it.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	// The fence connects west toward the stone.
	fenceWest := fenceState(t, map[string]string{"west": "true"})
	if got := watcher.ExpectBlockUpdate(); got.State != fenceWest {
		t.Errorf("fence next to stone state = %d, want %d (west=true)", got.State, fenceWest)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceWest {
		t.Errorf("world fence next to stone = %d, want %d", got, fenceWest)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestFenceConnectsToGlass checks that a fence connects to glass, which is
// transparent for light but has a full-cube collision shape: vanilla's
// isFaceSturdy check is about the collision shape, not light transparency.
func TestFenceConnectsToGlass(t *testing.T) {
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

	// Place glass at (5, 64, 5), then a fence at (6, 64, 5) next to it.
	glass, _ := registry.ItemID("minecraft:glass")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: glass, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	// The fence connects west toward the glass (sturdy face).
	fenceWest := fenceState(t, map[string]string{"west": "true"})
	if got := watcher.ExpectBlockUpdate(); got.State != fenceWest {
		t.Errorf("fence next to glass state = %d, want %d (west=true)", got.State, fenceWest)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceWest {
		t.Errorf("world fence next to glass = %d, want %d", got, fenceWest)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestFenceDoesNotConnectToLeaves checks that a fence does not connect to
// leaves, even though leaves have a full-cube collision shape: they are in
// vanilla's isExceptionForConnection set, which FenceBlock.connectsTo excludes.
func TestFenceDoesNotConnectToLeaves(t *testing.T) {
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

	// Place leaves at (5, 64, 5), then a fence at (6, 64, 5) next to it.
	leaves, _ := registry.ItemID("minecraft:oak_leaves")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: leaves, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	// The fence does NOT connect to leaves (exception); all connections false.
	fenceNone := fenceState(t, nil)
	if got := watcher.ExpectBlockUpdate(); got.State != fenceNone {
		t.Errorf("fence next to leaves state = %d, want %d (no connections)", got.State, fenceNone)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceNone {
		t.Errorf("world fence next to leaves = %d, want %d", got, fenceNone)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestFenceConnectsToFenceGateSidesOnly checks that a fence connects to a
// fence gate only from the two sides perpendicular to the gate's facing, not
// from the front or back. A gate facing north is oriented along the Z axis, so
// a fence to its east connects while a fence to its north does not.
func TestFenceConnectsToFenceGateSidesOnly(t *testing.T) {
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

	// Place a fence gate facing north: the player faces south (yaw=0), and
	// facingFromPlayer returns the opposite, "north". The gate goes at
	// (5, 64, 5); it is oriented along the Z axis, posts on the east/west.
	gate, _ := registry.ItemID("minecraft:oak_fence_gate")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: gate, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.FacePlayer(4.5, 64, 6.5, 0, 0)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // the fence gate itself (rotation skipped)

	// Place a fence to the EAST of the gate, at (6, 64, 5). The fence
	// connects west (toward the gate's side) — the gate's posts are on
	// east/west, so this is a side connection.
	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	fenceWest := fenceState(t, map[string]string{"west": "true"})
	if got := watcher.ExpectBlockUpdate(); got.State != fenceWest {
		t.Errorf("fence east of gate state = %d, want %d (west=true, side)", got.State, fenceWest)
	}
	if got := srv.BlockAt(6, 64, 5); got != fenceWest {
		t.Errorf("world fence east of gate = %d, want %d", got, fenceWest)
	}

	// Place a fence to the NORTH of the gate, at (5, 64, 4). The fence
	// should NOT connect south (toward the gate's front) — the gate faces
	// north, so the north side is the front, not a side.
	actor.PlaceBlock(5, 63, 4, protocol.FaceTop)
	fenceNone := fenceState(t, nil)
	if got := watcher.ExpectBlockUpdate(); got.State != fenceNone {
		t.Errorf("fence north of gate state = %d, want %d (no connections, front)", got.State, fenceNone)
	}
	if got := srv.BlockAt(5, 64, 4); got != fenceNone {
		t.Errorf("world fence north of gate = %d, want %d", got, fenceNone)
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

// TestPlacingAStairBesideAnotherShapesTheNewStair covers the issue's repro: a
// south-facing stair with a stair placed to its east looking west gives the
// new stair an outer_left corner, the stair it leans on staying straight.
// vanilla: StairBlock.getStateForPlacement
func TestPlacingAStairBesideAnotherShapesTheNewStair(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	actor.MoveToGround(5.5, 64, 4.5, true)
	actor.Settle()

	stairs, _ := registry.ItemID("minecraft:oak_stairs")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stairs, Count: 1})
	watcher.ExpectEquipment(entityID)

	// The first stair stands at (5, 64, 5) facing south.
	actor.FacePlayer(5.5, 64, 4.5, 0, 0)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	got := watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 ||
		got.State != stairsState(t, map[string]string{"facing": "south", "shape": "straight"}) {
		t.Fatalf("first stair = (%d,%d,%d) state %d, want (5,64,5) south straight", got.X, got.Y, got.Z, got.State)
	}

	// The stair to its east, placed looking west, faces west; the south
	// stair beside it is on its counter-clockwise side, an outer left.
	actor.FacePlayer(5.5, 64, 4.5, 90, 0)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	got = watcher.ExpectBlockUpdate()
	if got.X != 6 || got.Y != 64 || got.Z != 5 ||
		got.State != stairsState(t, map[string]string{"facing": "west", "shape": "outer_left"}) {
		t.Fatalf("new stair = (%d,%d,%d) state %d, want (6,64,5) west outer_left", got.X, got.Y, got.Z, got.State)
	}

	// The stair the new one leans on is on neither of its sides, so it
	// stays straight.
	if want := stairsState(t, map[string]string{"facing": "south", "shape": "straight"}); srv.BlockAt(5, 64, 5) != want {
		t.Errorf("leaned-on stair = %d, want %d (south straight)", srv.BlockAt(5, 64, 5), want)
	}
	if want := stairsState(t, map[string]string{"facing": "west", "shape": "outer_left"}); srv.BlockAt(6, 64, 5) != want {
		t.Errorf("new stair in world = %d, want %d (west outer_left)", srv.BlockAt(6, 64, 5), want)
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

// TestSetblockShapesStairs checks that /setblock gives a stair the corner shape
// the stair beside it asks for and reshapes that neighbour in turn, on the wire
// and in the world.
func TestSetblockShapesStairs(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	// An oak stair facing north at (5, 64, 5), straight: nothing is beside it.
	c.ChatCommand("setblock 5 64 5 oak_stairs[facing=north]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, stairsState(t, map[string]string{"facing": "north", "shape": "straight"}))

	// A stair to its north facing east: /setblock shapes the new stair
	// straight (the existing stair is south of it, on neither of its sides)
	// and reshapes the existing one into the outer corner.
	c.ChatCommand("setblock 5 64 4 oak_stairs[facing=east]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, stairsState(t, map[string]string{"facing": "east", "shape": "straight"}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}))

	if got := srv.BlockAt(5, 64, 4); got != stairsState(t, map[string]string{"facing": "east", "shape": "straight"}) {
		t.Errorf("new stair = %d, want east straight", got)
	}
	if got := srv.BlockAt(5, 64, 5); got != stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}) {
		t.Errorf("existing stair = %d, want north outer_right", got)
	}
}

// TestSetblockReshapeKeepsWaterlogged checks that a waterlogged stair keeps its
// waterlogged property when /setblock reshapes it from the stair beside it.
func TestSetblockReshapeKeepsWaterlogged(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_stairs[facing=north,waterlogged=true]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5,
		stairsState(t, map[string]string{"facing": "north", "shape": "straight", "waterlogged": "true"}))

	c.ChatCommand("setblock 5 64 4 oak_stairs[facing=east]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, stairsState(t, map[string]string{"facing": "east", "shape": "straight"}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5,
		stairsState(t, map[string]string{"facing": "north", "shape": "outer_right", "waterlogged": "true"}))

	want := stairsState(t, map[string]string{"facing": "north", "shape": "outer_right", "waterlogged": "true"})
	if got := srv.BlockAt(5, 64, 5); got != want {
		t.Errorf("waterlogged stair reshaped to %d, want %d", got, want)
	}
}

// TestFillShapesStairs checks that a non-strict /fill gives a filled stair the
// corner shape the stair beside it asks for and reshapes that neighbour.
func TestFillShapesStairs(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 oak_stairs[facing=north]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, stairsState(t, map[string]string{"facing": "north", "shape": "straight"}))

	c.ChatCommand("fill 5 64 4 5 64 4 oak_stairs[facing=east]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, stairsState(t, map[string]string{"facing": "east", "shape": "straight"}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}))

	if got := srv.BlockAt(5, 64, 5); got != stairsState(t, map[string]string{"facing": "north", "shape": "outer_right"}) {
		t.Errorf("existing stair = %d, want north outer_right", got)
	}
}

// TestPlacingFenceAtBlockEdge checks that a fence can be placed on a block the
// player is standing on when the player is outside the fence's thin collision
// shape, the way the vanilla server's shape-based placement test allows. A
// full-cube block such as stone in the same position is still rejected, since
// the player's body overlaps the full cube.
//
// The fence's post is 4/16 wide (6/16 to 10/16), so a player whose 0.6-wide box
// ends before 6/16 (= 0.375) from the block corner does not overlap. The player
// stands at x+0.05, giving a box ending at x+0.35 — before the post at x+0.375.
// With the wrong post (5/16 to 11/16) this would have been rejected, since
// 0.35 > 0.3125; the correct dimensions give 0.35 < 0.375.
func TestPlacingFenceAtBlockEdge(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Stand the actor slightly inside the block on X but centred on Z: feet
	// at (10.05, 64, 10.5). The player's box is X [9.75, 10.35], Z [10.2, 10.8].
	// The post is X [10.375, 10.625], Z [10.375, 10.625]. The box's X max (10.35)
	// is below the post's X min (10.375), so there is no overlap on X.
	// The watcher stays at spawn (0.5, 64, 0.5), far enough not to obstruct.
	const x, y, z = int32(10), int32(64), int32(10)
	actor.MoveTo(float64(x)+0.05, float64(y), float64(z)+0.5, 0, 0)
	actor.Settle()
	// Drain the entity-movement packets the actor's move sent to the watcher,
	// so they do not land between the equipment update and the reader.
	watcher.Settle()

	// Place a fence on top of the stone at (10, 63, 10). The fence goes at
	// (10, 64, 10) - where the actor's feet are, but outside the post.
	fence, _ := registry.ItemID("minecraft:oak_fence")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: fence, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.PlaceBlock(x, y-1, z, protocol.FaceTop)

	// The placement succeeds: the watcher sees the fence appear.
	fenceNone := fenceState(t, nil)
	got := watcher.ExpectBlockUpdate()
	if got.X != x || got.Y != y || got.Z != z {
		t.Errorf("fence placed at (%d,%d,%d), want (%d,%d,%d)", got.X, got.Y, got.Z, x, y, z)
	}
	if got.State != fenceNone {
		t.Errorf("fence state = %d, want %d (no connections)", got.State, fenceNone)
	}
	if got := srv.BlockAt(x, y, z); got != fenceNone {
		t.Errorf("world fence at edge = %d, want %d", got, fenceNone)
	}

	// Break the fence so the target is air again, then try placing a stone in
	// the same position. The full cube overlaps the player's body, so the
	// placement is rejected.
	actor.BreakBlock(x, y, z)
	watcher.ExpectBlockUpdate() // the fence becomes air

	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.PlaceBlock(x, y-1, z, protocol.FaceTop)

	// The stone placement is rejected: only an ack, no block update.
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("stone ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("world after rejected stone place = %d, want 0 (air)", got)
	}

	// The watcher sees nothing for the rejected placement: a ping round-trip
	// with no intervening block update means there was none.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

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

// TestBarsConnectToGlass checks that iron bars connect to glass, which has a
// sturdy face (full-cube collision) despite being transparent for light.
func TestBarsConnectToGlass(t *testing.T) {
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

	glass, _ := registry.ItemID("minecraft:glass")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: glass, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	bars, _ := registry.ItemID("minecraft:iron_bars")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: bars, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	barsWest := barsState(t, map[string]string{"west": "true"})
	if got := watcher.ExpectBlockUpdate(); got.State != barsWest {
		t.Errorf("bars next to glass state = %d, want %d (west=true)", got.State, barsWest)
	}
	if got := srv.BlockAt(6, 64, 5); got != barsWest {
		t.Errorf("world bars next to glass = %d, want %d", got, barsWest)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestBarsDoNotConnectToFence checks that iron bars do not connect to a fence:
// the vanilla IronBarsBlock.attachsTo checks instanceof IronBarsBlock, and a
// fence is not one.
func TestBarsDoNotConnectToFence(t *testing.T) {
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
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	bars, _ := registry.ItemID("minecraft:iron_bars")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: bars, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	barsNone := barsState(t, nil)
	if got := watcher.ExpectBlockUpdate(); got.State != barsNone {
		t.Errorf("bars next to fence state = %d, want %d (no connections)", got.State, barsNone)
	}
	if got := srv.BlockAt(6, 64, 5); got != barsNone {
		t.Errorf("world bars next to fence = %d, want %d", got, barsNone)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingBarsAtBlockEdge checks that iron bars can be placed on a block the
// player is standing on when the player is outside the bars' thin collision
// shape. The bars' post is only 2/16 wide (7/16 to 9/16), thinner than a
// fence's 4/16, so the player has even more room.
func TestPlacingBarsAtBlockEdge(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Stand the actor slightly inside the block on X: feet at (10.05, 64, 10.5).
	// The player's box is X [9.75, 10.35]. The bars' post is X [10.4375, 10.5625].
	// The box's X max (10.35) is below the post's X min (10.4375), so no overlap.
	const x, y, z = int32(10), int32(64), int32(10)
	actor.MoveTo(float64(x)+0.05, float64(y), float64(z)+0.5, 0, 0)
	actor.Settle()
	watcher.Settle()

	bars, _ := registry.ItemID("minecraft:iron_bars")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: bars, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.PlaceBlock(x, y-1, z, protocol.FaceTop)

	barsNone := barsState(t, nil)
	got := watcher.ExpectBlockUpdate()
	if got.X != x || got.Y != y || got.Z != z {
		t.Errorf("bars placed at (%d,%d,%d), want (%d,%d,%d)", got.X, got.Y, got.Z, x, y, z)
	}
	if got.State != barsNone {
		t.Errorf("bars state = %d, want %d (no connections)", got.State, barsNone)
	}
	if got := srv.BlockAt(x, y, z); got != barsNone {
		t.Errorf("world bars at edge = %d, want %d", got, barsNone)
	}

	// Break the bars, then try placing stone in the same position. The full
	// cube overlaps the player's body, so the placement is rejected.
	actor.BreakBlock(x, y, z)
	watcher.ExpectBlockUpdate()

	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.PlaceBlock(x, y-1, z, protocol.FaceTop)

	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("stone ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("world after rejected stone place = %d, want 0 (air)", got)
	}

	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

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

// TestWallConnectsToStone checks that a wall connects to a solid block such as
// stone, which has a sturdy face.
func TestWallConnectsToStone(t *testing.T) {
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

	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	wall, _ := registry.ItemID("minecraft:cobblestone_wall")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wall, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	// The wall connects west toward the stone (low arm, no block above).
	// With only west connected (a corner), the post is up.
	wallWestLow := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	})
	if got := watcher.ExpectBlockUpdate(); got.State != wallWestLow {
		t.Errorf("wall next to stone state = %d, want %d (west=low)", got.State, wallWestLow)
	}
	if got := srv.BlockAt(6, 64, 5); got != wallWestLow {
		t.Errorf("world wall next to stone = %d, want %d", got, wallWestLow)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestWallDoesNotConnectToFence checks that a wall does not connect to a fence
// and a fence does not connect to a wall: vanilla WallBlock.connectsTo checks
// walls, iron bars, fence gates, and sturdy faces (not fences), and
// FenceBlock.connectsTo checks same-group fences, fence gates, and sturdy
// faces (not walls). Neither block changes when the other is placed next to
// it.
func TestWallDoesNotConnectToFence(t *testing.T) {
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
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()

	wall, _ := registry.ItemID("minecraft:cobblestone_wall")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wall, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)

	// The wall does NOT connect to the fence; post is up (all sides none =
	// corner), all sides are none.
	wallPost := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	})
	if got := watcher.ExpectBlockUpdate(); got.State != wallPost {
		t.Errorf("wall next to fence state = %d, want %d (no connections)", got.State, wallPost)
	}
	if got := srv.BlockAt(6, 64, 5); got != wallPost {
		t.Errorf("world wall next to fence = %d, want %d", got, wallPost)
	}
	// The fence at (5, 64, 5) does NOT connect to the wall either
	// (FenceBlock.connectsTo has no wall check), so it keeps its default
	// state with all connections false and gets no neighbour update.
	fenceNone := fenceState(t, map[string]string{
		"north": "false", "south": "false", "east": "false", "west": "false",
	})
	if got := srv.BlockAt(5, 64, 5); got != fenceNone {
		t.Errorf("world fence next to wall = %d, want %d (no connections)", got, fenceNone)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestBreakingAWallUpdatesNeighbours checks that breaking a wall reconnects
// the walls left behind: a wall next to the broken one loses the connection
// that pointed at it and raises its post.
func TestBreakingAWallUpdatesNeighbours(t *testing.T) {
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

	// Place two connected walls at (5, 64, 5) and (6, 64, 5).
	wall, _ := registry.ItemID("minecraft:cobblestone_wall")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wall, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate()
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // new wall (west=low)
	watcher.ExpectBlockUpdate() // first wall reconnected (east=low)

	// Break the second wall. The first wall should lose its east connection
	// and raise its post (all sides none = corner).
	actor.BreakBlock(6, 64, 5)
	watcher.ExpectBlockUpdate() // broken wall becomes air

	wallPost := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	})
	got := watcher.ExpectBlockUpdate()
	if got.X != 5 || got.Y != 64 || got.Z != 5 {
		t.Errorf("reconnect update at (%d,%d,%d), want (5,64,5)", got.X, got.Y, got.Z)
	}
	if got.State != wallPost {
		t.Errorf("reconnected wall state = %d, want %d (post up, no connections)", got.State, wallPost)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestWallStackedAboveWall checks that when a wall is placed above another
// wall, the lower wall's arms become tall (covered by the upper wall's arms)
// and the post is raised, matching vanilla's WallBlock.updateShape cascade.
// In vanilla, a wall's side is tall when isCovered(aboveShape, armTestShape)
// is true — the block above's collision shape covers the arm test shape. A
// wall above with an arm on the same direction covers that arm, making the
// lower wall's arm tall.
func TestWallStackedAboveWall(t *testing.T) {
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

	wall, _ := registry.ItemID("minecraft:cobblestone_wall")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wall, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Place two connected walls at y=64: (5, 64, 5) and (6, 64, 5).
	// Both have low arms (no block above) and posts up (corners).
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // (5, 64, 5) new wall: post up, all none
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // (6, 64, 5) new wall: west=low, post up
	watcher.ExpectBlockUpdate() // (5, 64, 5) reconnected: east=low, post up

	// Place two connected walls at y=65 directly above: (5, 65, 5) and
	// (6, 65, 5). The upper walls connect to each other (low arms).
	actor.PlaceBlock(5, 64, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // (5, 65, 5) new wall: post up, all none
	// The neighbour shape pass reconnects the wall below (5, 64, 5): the wall
	// above has all sides none, so the lower wall's east arm stays low.
	// No block update for (5, 64, 5) — state didn't change.

	actor.PlaceBlock(6, 64, 5, protocol.FaceTop)
	watcher.ExpectBlockUpdate() // (6, 65, 5) new wall: west=low, post up
	watcher.ExpectBlockUpdate() // (5, 65, 5) reconnected: east=low, post up
	watcher.ExpectBlockUpdate() // (5, 64, 5) cascaded: east=tall, post up
	watcher.ExpectBlockUpdate() // (6, 64, 5) reconnected: west=tall, post up

	// The lower walls' arms should be tall: the upper walls have arms on
	// the same directions, which cover the lower walls' arm test shapes.
	wallEastTall := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "tall", "west": "none",
	})
	if got := srv.BlockAt(5, 64, 5); got != wallEastTall {
		t.Errorf("lower wall (5,64,5) = %d, want %d (east=tall)", got, wallEastTall)
	}
	wallWestTall := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "tall",
	})
	if got := srv.BlockAt(6, 64, 5); got != wallWestTall {
		t.Errorf("lower wall (6,64,5) = %d, want %d (west=tall)", got, wallWestTall)
	}

	// The upper walls should have low arms (no block above them).
	wallEastLow := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "none",
	})
	if got := srv.BlockAt(5, 65, 5); got != wallEastLow {
		t.Errorf("upper wall (5,65,5) = %d, want %d (east=low)", got, wallEastLow)
	}
	wallWestLow := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	})
	if got := srv.BlockAt(6, 65, 5); got != wallWestLow {
		t.Errorf("upper wall (6,65,5) = %d, want %d (west=low)", got, wallWestLow)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestAnLShapedWallRaisesItsCornerPost checks that a wall bending a corner
// raises the post where its two arms meet, the not-a-straight-run half of
// vanilla's WallBlock.shouldRaisePost. The L is (5,64,5) with an arm east to
// (6,64,5) and an arm north to (5,64,4).
func TestAnLShapedWallRaisesItsCornerPost(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	c.ChatCommand("setblock 5 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	}))
	c.ChatCommand("setblock 6 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 6, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "none",
	}))
	c.ChatCommand("setblock 5 64 4 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "low", "east": "none", "west": "none",
	}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "low", "south": "none", "east": "low", "west": "none",
	}))

	corner := wallState(t, map[string]string{
		"up": "true", "north": "low", "south": "none", "east": "low", "west": "none",
	})
	if got := srv.BlockAt(5, 64, 5); got != corner {
		t.Errorf("L corner = %d, want %d (post raised)", got, corner)
	}
}

// TestATorchRaisesAStraightWallsMiddlePost checks the #wall_post_override half
// of vanilla's WallBlock.shouldRaisePost: a torch on the middle of a straight
// run raises the post under it, where the run alone stays flat.
func TestATorchRaisesAStraightWallsMiddlePost(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))

	// A straight run along x: (5,64,5), (6,64,5), (7,64,5). The middle wall
	// has two matching arms once the run is complete, so it stays flat.
	c.ChatCommand("setblock 5 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "none",
	}))
	c.ChatCommand("setblock 6 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 6, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	}))
	servertest.AwaitBlockUpdate(t, c, 5, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "none",
	}))
	c.ChatCommand("setblock 7 64 5 cobblestone_wall")
	servertest.AwaitBlockUpdate(t, c, 7, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "none", "west": "low",
	}))
	servertest.AwaitBlockUpdate(t, c, 6, 64, 5, wallState(t, map[string]string{
		"up": "false", "north": "none", "south": "none", "east": "low", "west": "low",
	}))

	// The torch above the middle wall puts that wall's post up.
	c.ChatCommand("setblock 6 65 5 torch")
	servertest.AwaitBlockUpdate(t, c, 6, 64, 5, wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "low",
	}))

	post := wallState(t, map[string]string{
		"up": "true", "north": "none", "south": "none", "east": "low", "west": "low",
	})
	if got := srv.BlockAt(6, 64, 5); got != post {
		t.Errorf("straight wall under a torch = %d, want %d (post raised)", got, post)
	}
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

// TestPickBlockUsesNextEmptyHotbarSlot checks that middle-clicking a block when
// the selected slot is occupied but a later hotbar slot is free puts the new
// item in the next empty slot and switches to it, rather than overwriting the
// held item, and that the other players see the new held item.
func TestPickBlockUsesNextEmptyHotbarSlot(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a diamond in the selected slot (0), the way a creative client does.
	diamond, _ := registry.ItemID("minecraft:diamond")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Middle-click the stone at the actor's feet. The stone is not on the
	// hotbar, and the selected slot is occupied, so the stone should go in
	// the next empty slot (index 1) and the selection should move to it.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.PickBlock(0, 63, 0)

	// The actor hears both a Set Container Slot for slot 37 and a Set Held
	// Item for slot 1, in either order.
	sawHeld, sawSlot := false, false
	for gotID, data := range actor.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayUpdateTime:
			// A periodic time sync that fell between the pick block and this
			// read; skip it.
		case protocol.IDPlayHeldItem:
			if got := protocol.NewReader(data).ReadVarInt(); got != 1 {
				t.Errorf("held item = %d, want 1 (the next empty slot)", got)
			}
			sawHeld = true
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt() // window ID
			r.ReadVarInt() // state ID
			if got := r.ReadI16(); got != player.HotbarSlot+1 {
				t.Errorf("container slot = %d, want %d (the next empty slot)", got, player.HotbarSlot+1)
			}
			stack, _ := r.ReadSlot()
			if stack.ItemID != stone || stack.Count != 1 {
				t.Errorf("slot = %+v, want a single stone", stack)
			}
			if err := r.Err(); err != nil {
				t.Fatalf("set container slot: %v", err)
			}
			sawSlot = true
		default:
			t.Fatalf("unexpected packet 0x%02X after pick block", gotID)
		}
		if sawHeld && sawSlot {
			break
		}
	}
	if !sawHeld || !sawSlot {
		t.Fatalf("pick block response incomplete within %s (held %v, slot %v)", servertest.ExpectTimeout, sawHeld, sawSlot)
	}

	// The other players see the actor now holding a stone, not the diamond.
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand = %+v, want the stone in the new slot", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockConjureMovesTheHeldStackAside checks that a creative conjure
// over a full hotbar moves the held stack to the first free slot instead of
// destroying it: the held diamonds land in the first main-inventory slot,
// and both rewritten slots are told to the client.
func TestPickBlockConjureMovesTheHeldStackAside(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// A full hotbar of diamonds, the way a creative client fills it from
	// the menu; the selected slot 0 holds one of them.
	diamond, _ := registry.ItemID("minecraft:diamond")
	for slot := player.HotbarSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		actor.SetCreativeSlot(int16(slot), protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	watcher.ExpectEquipment(entityID)

	// Middle-click the stone at the actor's feet. Nothing carried matches,
	// so the conjure moves the held diamonds to the first free slot - the
	// first main-inventory slot, the hotbar being full - and puts the
	// stone in the hand.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.PickBlock(0, 63, 0)

	// The hotbar is full, so the suitable slot is the selected one and Set
	// Held Item keeps it.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 0 {
		t.Errorf("held item = %d, want 0 (the hotbar is full)", got)
	}

	// Both rewritten slots follow, the held one first.
	for _, want := range []struct {
		slot  int16
		stack protocol.ItemStack
	}{
		{player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1}},
		{player.MainSlot, protocol.ItemStack{ItemID: diamond, Count: 1}},
	} {
		r := protocol.NewReader(actor.ExpectPlay(protocol.IDPlayContainerSlot))
		if got := r.ReadVarInt(); got != 0 {
			t.Errorf("window ID = %d, want 0 (the player's inventory)", got)
		}
		r.ReadVarInt() // state ID
		if got := r.ReadI16(); got != want.slot {
			t.Errorf("slot = %d, want %d", got, want.slot)
		}
		stack, _ := r.ReadSlot()
		if stack.ItemID != want.stack.ItemID || stack.Count != want.stack.Count {
			t.Errorf("slot %d = %+v, want %+v", want.slot, stack, want.stack)
		}
		if err := r.Err(); err != nil {
			t.Fatalf("set container slot: %v", err)
		}
	}

	// The other players see the actor now holding the stone.
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand = %+v, want the stone", got)
	}

	// And the move is real: the diamonds survive in the first free slot.
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != stone || got.Count != 1 {
		t.Errorf("held slot = %+v, want the stone", got)
	}
	if got := srv.InventorySlot("Actor", player.MainSlot); got.ItemID != diamond || got.Count != 1 {
		t.Errorf("first main slot = %+v, want the held diamond moved there", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockSwitchesToExistingHotbarSlot checks that middle-clicking a block
// whose item is already somewhere on the hotbar moves the selection to that
// slot rather than creating a new stack, and that the other players see the
// new held item.
func TestPickBlockSwitchesToExistingHotbarSlot(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a stone in hotbar slot 3 and a diamond in slot 0 (the selected
	// slot), the way a creative client fills the hotbar from the menu.
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.SetCreativeSlot(player.HotbarSlot+3, protocol.ItemStack{ItemID: stone, Count: 1})
	// Slot 3 is not the selected slot, so the other players do not see it.

	// Middle-click the stone at the actor's feet. The stone is already on
	// the hotbar (slot 3), so this should switch the selection to it.
	actor.PickBlock(0, 63, 0)

	// The actor is told the selection moved to slot 3.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 3 {
		t.Errorf("held item = %d, want 3 (the slot the stone is in)", got)
	}

	// The other players see the actor now holding a stone rather than the
	// diamond.
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand = %+v, want the stone from slot 3", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockNoopWhenAlreadyHolding checks that middle-clicking a block whose
// item is already in the selected slot changes nothing: the inventory and
// equipment stay as they are, and only the Set Held Item vanilla sends on
// every accepted pick reaches the actor.
func TestPickBlockNoopWhenAlreadyHolding(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Put a stone in the selected slot (0), the way a creative client does.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)

	// Middle-click the stone at the actor's feet. The actor is already
	// holding stone, so nothing changes.
	actor.PickBlock(0, 63, 0)

	// The actor hears only Set Held Item for the slot it already holds. A
	// ping flushes anything after it, and expectPlay fails if an inventory
	// packet turns up instead of the answer.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 0 {
		t.Errorf("held item = %d, want 0 (the slot it already holds)", got)
	}
	actor.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	actor.ExpectPlay(protocol.IDPlayPingResponse)

	// The watcher sees nothing either. Probed the same way: a ping
	// round-trip with no intervening equipment packet means there was none.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockSurvivalDoesNotCreateTheItem checks that middle-clicking a
// block whose item a survival player does not carry changes nothing: no
// stack is created, no slot is rewritten and the selection keeps the held
// item - survival pick block only selects or swaps in a stack the player
// already has, where creative conjures one.
func TestPickBlockSurvivalDoesNotCreateTheItem(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// A diamond on the hotbar, and no stone anywhere on them.
	diamond, _ := registry.ItemID("minecraft:diamond")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})

	// Middle-click the stone at the actor's feet. Stone is not in the
	// inventory, so survival picks nothing: the selection keeps the
	// diamond and no slot is rewritten.
	actor.PickBlock(0, 63, 0)

	// The actor hears Set Held Item for the slot it already held, and
	// nothing else. A ping flushes anything after it, and expectPlay
	// fails if an inventory packet turns up instead of the answer.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 0 {
		t.Errorf("held item = %d, want 0 (the selection keeps the diamond)", got)
	}
	actor.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	actor.ExpectPlay(protocol.IDPlayPingResponse)

	// The watcher sees nothing either: the hand still holds the diamond.
	watcher.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	watcher.ExpectPlay(protocol.IDPlayPingResponse)

	// And the inventory is as it was: the diamond still in its slot, no
	// stone anywhere a pick could have put one, the 27 main slots included.
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != diamond || got.Count != 1 {
		t.Errorf("held slot = %+v, want the diamond untouched", got)
	}
	for slot := player.HotbarSlot + 1; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		if got := srv.InventorySlot("Actor", slot); !got.Empty() {
			t.Errorf("hotbar slot %d = %+v, want it still empty", slot, got)
		}
	}
	for slot := player.MainSlot; slot < player.MainSlot+27; slot++ {
		if got := srv.InventorySlot("Actor", slot); !got.Empty() {
			t.Errorf("main slot %d = %+v, want the inventory unchanged", slot, got)
		}
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

// TestPickBlockSurvivalSwitchesToExistingHotbarSlot checks that
// middle-clicking a block whose item is already somewhere on a survival
// player's hotbar moves the selection to that slot and nothing else - the
// same pick a creative player makes.
func TestPickBlockSurvivalSwitchesToExistingHotbarSlot(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// A diamond held in the selected slot, and stone in hotbar slot 3.
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	srv.GiveSlot("Actor", player.HotbarSlot+3, protocol.ItemStack{ItemID: stone, Count: 12})

	// Middle-click the stone at the actor's feet. The stone is already on
	// the hotbar, so the selection moves to it.
	actor.PickBlock(0, 63, 0)

	// The selection moves to slot 3, and no slot was rewritten.
	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 3 {
		t.Errorf("held item = %d, want 3 (the slot the stone is in)", got)
	}
	actor.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	actor.ExpectPlay(protocol.IDPlayPingResponse)

	// The other players see the actor now holding the stone.
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 12 {
		t.Errorf("main hand = %+v, want the stone from slot 3", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPickBlockRedstoneWireGivesRedstone checks that middle-clicking a
// redstone wire conjures redstone: vanilla's getCloneItemStack does not read
// the item off the block's name, and the wire has no item of its own.
func TestPickBlockRedstoneWireGivesRedstone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, actor := servertest.Join(t, cfg, "Actor")

	// The wire sits in the actor's feet cell, the way a placed one would,
	// with the ground stone below to hold it.
	wire := registry.AllStates["minecraft:redstone_wire"][0].ID
	srv.SetBlock(0, 64, 0, wire)

	redstone, _ := registry.ItemID("minecraft:redstone")
	actor.PickBlock(0, 64, 0)

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
	if stack.ItemID != redstone || stack.Count != 1 {
		t.Errorf("slot = %+v, want a single redstone", stack)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set container slot: %v", err)
	}

	actor.Conn.Close()
}

// TestPickBlockWallTorchGivesTorch checks that middle-clicking a wall torch
// conjures a torch: the wall variant has no item of its own, the standing
// torch it hangs off does.
func TestPickBlockWallTorchGivesTorch(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, actor := servertest.Join(t, cfg, "Actor")

	// The torch hangs off the south face of a stone set beside the actor,
	// the way a placed one would.
	stone := registry.BlockStates["minecraft:stone"]
	srv.SetBlock(1, 64, 0, stone)
	torch, ok := registry.FindState("minecraft:wall_torch", map[string]string{"facing": "south"})
	if !ok {
		t.Fatalf("wall_torch facing=south: not found")
	}
	srv.SetBlock(1, 64, 1, torch)

	want, _ := registry.ItemID("minecraft:torch")
	actor.PickBlock(1, 64, 1)

	heldData := actor.ExpectPlay(protocol.IDPlayHeldItem)
	if got := protocol.NewReader(heldData).ReadVarInt(); got != 0 {
		t.Errorf("held item = %d, want 0 (the selection stays put)", got)
	}
	slotData := actor.ExpectPlay(protocol.IDPlayContainerSlot)
	r := protocol.NewReader(slotData)
	r.ReadVarInt() // window ID
	r.ReadVarInt() // state ID
	r.ReadI16()    // slot
	stack, _ := r.ReadSlot()
	if stack.ItemID != want || stack.Count != 1 {
		t.Errorf("slot = %+v, want a single torch", stack)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set container slot: %v", err)
	}

	actor.Conn.Close()
}

// TestPickBlockWithoutAnItemPicksNothing checks that middle-clicking a block
// with no item form - water, like the portals and the moving piston - is
// refused: nothing goes to the hand and nothing is rewritten.
func TestPickBlockWithoutAnItemPicksNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, actor := servertest.Join(t, cfg, "Actor")

	// Water fills the actor's feet cell, the way a world generator would.
	water := registry.AllStates["minecraft:water"][0].ID
	srv.SetBlock(0, 64, 0, water)

	actor.PickBlock(0, 64, 0)
	actor.ExpectNoneWithinTicks(srv, servertest.QuietTicks,
		servertest.PacketIs(protocol.IDPlayHeldItem, protocol.IDPlayContainerSlot))

	actor.Conn.Close()
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

// TestPlacingABlockDoesNotReplaceAFlower checks that a block placed by clicking
// on a flower goes in the adjacent position (above the flower), not replacing
// the flower: flowers are not in the minecraft:replaceable tag, so the vanilla
// server treats the clicked position as occupied and the new block goes next
// to it.
func TestPlacingABlockDoesNotReplaceAFlower(t *testing.T) {
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

	// Place dirt at (5, 64, 5) on the stone, then a dandelion on the dirt
	// (a dandelion refuses stone).
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

	dandelion, _ := registry.ItemID("minecraft:dandelion")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: dandelion, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 64, 5, protocol.FaceTop)

	dandelionState := registry.BlockStates["minecraft:dandelion"]
	if got := watcher.ExpectBlockUpdate(); got.State != dandelionState {
		t.Errorf("dandelion state = %d, want %d", got.State, dandelionState)
	}
	if got := actor.ExpectBlockChange(); got.State != dandelionState {
		t.Errorf("actor dandelion state = %d, want %d", got.State, dandelionState)
	}

	// Switch to stone and click on the flower itself. The flower is NOT
	// replaceable, so the stone goes in the adjacent position (above the
	// flower, at (5, 66, 5)), not replacing the flower.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(5, 65, 5, protocol.FaceTop)

	// The watcher sees the stone appear above the flower, at (5, 66, 5).
	got := watcher.ExpectBlockUpdate()
	if got.State != 1 {
		t.Errorf("stone state = %d, want 1 (stone)", got.State)
	}
	if got.X != 5 || got.Y != 66 || got.Z != 5 {
		t.Errorf("stone placed at (%d,%d,%d), want (5,66,5) (above the flower)", got.X, got.Y, got.Z)
	}
	if got := actor.ExpectBlockChange(); got.State != 1 {
		t.Errorf("actor stone state = %d, want 1 (stone)", got.State)
	}

	// The world still has the dandelion at (5, 65, 5) and stone above it.
	if got := srv.BlockAt(5, 65, 5); got != dandelionState {
		t.Errorf("world block at (5,65,5) = %d, want %d (dandelion, not replaced)", got, dandelionState)
	}
	if got := srv.BlockAt(5, 66, 5); got != 1 {
		t.Errorf("world block at (5,66,5) = %d, want 1 (stone above flower)", got)
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

// TestAUseItemOnWithAnOutOfRangeHandOrFaceIsReadByID clicks with a hand
// and faces outside their enums: hand 2 is the main hand, face 7 wraps to
// the top and face -1 to the east, so both clicks build and the sender stays.
// vanilla: InteractionHand.BY_ID, Direction.BY_ID
func TestAUseItemOnWithAnOutOfRangeHandOrFaceIsReadByID(t *testing.T) {
	t.Parallel()
	srv, builder, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Builder")
	builder.MoveToGround(2.5, 64, 5.5, true)
	builder.Settle()

	srv.SetBlock(4, 65, 5, servertest.BlockState(t, "minecraft:stone"))
	dirt := servertest.MustItemID(t, "minecraft:dirt")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: dirt, Count: 2})

	for _, click := range []struct {
		face    int32
		x, y, z int32
	}{
		{7, 4, 66, 5},
		{-1, 5, 65, 5},
	} {
		builder.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
			b.PutVarInt(2) // hand, one past the off hand
			b.PutPosition(4, 65, 5)
			b.PutVarInt(click.face)
			b.PutF32(0.5)
			b.PutF32(0.5)
			b.PutF32(0.5)
			b.PutBool(false) // inside block
			b.PutBool(false) // world border hit
			b.PutVarInt(1)   // sequence
		})
		builder.ExpectBlockAck()
		if got := srv.BlockAt(click.x, click.y, click.z); got == 0 {
			t.Errorf("face %d built nothing at (%d, %d, %d)", click.face, click.x, click.y, click.z)
		}
	}
	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want the sender kept", got)
	}
}

// TestFillDestroyPopsBlocksAttachedToDestroyedCells checks that /fill destroy
// reshapes the neighbours of each cell it breaks, so a flower on a destroyed
// dirt cell and a torch on a destroyed stone cell pop and drop.
// vanilla: FillCommand.fillBlocks, Level.destroyBlock
func TestFillDestroyPopsBlocksAttachedToDestroyedCells(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	srv.StopTick()

	srv.SetBlock(5, 69, 5, servertest.MustState(t, "minecraft:dirt", nil))
	srv.SetBlock(5, 70, 5, servertest.MustState(t, "minecraft:poppy", nil))
	srv.SetBlock(8, 69, 5, servertest.MustState(t, "minecraft:stone", nil))
	srv.SetBlock(8, 70, 5, servertest.MustState(t, "minecraft:torch", nil))
	w.Settle()
	w.ChatCommand("fill 5 69 5 5 69 5 minecraft:coarse_dirt destroy")
	w.ChatCommand("fill 8 69 5 8 69 5 minecraft:dirt destroy")
	w.Settle()
	if got := srv.BlockAt(5, 70, 5); got != 0 {
		t.Errorf("poppy above a destroyed cell is block %d, want air", got)
	}
	if got := srv.BlockAt(8, 70, 5); got != 0 {
		t.Errorf("torch above a destroyed cell is block %d, want air", got)
	}
	if got := countItems(srv, "minecraft:poppy"); got != 1 {
		t.Errorf("%d poppies lie dropped, want 1", got)
	}
	if got := countItems(srv, "minecraft:torch"); got != 1 {
		t.Errorf("%d torches lie dropped, want 1", got)
	}
}
