package gamemode

// The gamemaster block tests: the command block, structure block, jigsaw and
// the two test block items are vanilla's GameMasterBlockItem, which refuses
// placement unless the placer can use the gamemaster blocks - creative mode
// and the gamemaster permission - while the block itself would stand for
// anyone who got one.

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// expectNothingPlaced reads whatever the client has queued and fails if it
// carries a block update at the cell or any sound effect: the two the server
// would have sent had the placement gone through.
func expectNothingPlaced(t *testing.T, c *servertest.Client, x, y, z int32) {
	t.Helper()
	var updates []servertest.BlockChange
	var sounds int
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			ux, uy, uz := r.ReadPosition()
			updates = append(updates, servertest.BlockChange{X: ux, Y: uy, Z: uz})
		case protocol.IDPlaySoundEffect:
			// The actor's footsteps into place are not the placement's.
			if !servertest.IsPlayerMovementSound(data) {
				sounds++
			}
		}
	})
	for _, u := range updates {
		if u.X == x && u.Y == y && u.Z == z {
			t.Errorf("a block update for a refused placement arrived at (%d,%d,%d)", x, y, z)
		}
	}
	if sounds > 0 {
		t.Errorf("a refused placement played %d sound effect(s)", sounds)
	}
}

// TestACreativeOperatorLaysAGameMasterBlock sets a command block down as a
// creative operator: the block appears for the watcher, who hears the place
// sound the placer's own client predicted.
func TestACreativeOperatorLaysAGameMasterBlock(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	commandBlock, _ := registry.ItemID("minecraft:command_block")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: commandBlock, Count: 1})
	watcher.ExpectEquipment(entityID)

	servertest.PrepareGameMasterPlacement(actor)

	actor.PlaceBlockAt(4, 63, 5, protocol.FaceTop, 0.5)
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("creative operator ack sequence = %d, want 1", seq)
	}
	actor.ExpectUseEchoes(4, 63, 5, protocol.FaceTop)

	// The sound goes out at once, the block with the tick's flush.
	watcher.ExpectPlaceSound(t, rules.BlockPlaceSound(registry.BlockCommandBlock).Name(), 4, 64, 5)
	if got := watcher.ExpectBlockUpdate(); got.X != 4 || got.Y != 64 || got.Z != 5 {
		t.Errorf("command block update at (%d,%d,%d), want (4,64,5)", got.X, got.Y, got.Z)
	}
	servertest.ExpectBlockNamed(t, srv, 4, 64, 5, "minecraft:command_block", nil)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestASurvivalOperatorCannotLayAGameMasterBlock is the creative half of the
// gate: an operator who carries a command block into survival has the
// placement refused, the world keeping its air and the other players hearing
// nothing.
func TestASurvivalOperatorCannotLayAGameMasterBlock(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The stack is taken into survival from the creative inventory, which is
	// the one place a survival player could not have gotten it.
	commandBlock, _ := registry.ItemID("minecraft:command_block")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: commandBlock, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.ChatCommand("gamemode survival")
	actor.Settle()
	watcher.Settle()

	servertest.PrepareGameMasterPlacement(actor)

	actor.PlaceBlockAt(4, 63, 5, protocol.FaceTop, 0.5)
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("survival operator ack sequence = %d, want 1", seq)
	}
	// The tail's echoes revert the prediction: the clicked stone, and the
	// cell the command block would have taken, still air.
	if echoes := actor.ExpectUseEchoes(4, 63, 5, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}
	if got := srv.BlockAt(4, 64, 5); got != 0 {
		t.Errorf("world after a survival operator's placement = %d, want 0 (air)", got)
	}
	expectNothingPlaced(t, watcher, 4, 64, 5)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestANonOperatorCannotLayAGameMasterBlock is the permission half: a player
// in creative without the gamemaster permission has the placement refused too,
// so the gate is the pairing, not either half alone.
func TestANonOperatorCannotLayAGameMasterBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Guest")

	commandBlock, _ := registry.ItemID("minecraft:command_block")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: commandBlock, Count: 1})

	servertest.PrepareGameMasterPlacement(c)

	c.PlaceBlockAt(4, 63, 5, protocol.FaceTop, 0.5)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("non-operator ack sequence = %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(4, 63, 5, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}
	if got := srv.BlockAt(4, 64, 5); got != 0 {
		t.Errorf("world after a non-operator's placement = %d, want 0 (air)", got)
	}
}

// TestANonOperatorCannotLayATestBlock repeats the gate for a test block, one
// of the two test blocks the item list covers beside the command blocks,
// structure block and jigsaw.
func TestANonOperatorCannotLayATestBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Guest")

	testBlock, _ := registry.ItemID("minecraft:test_block")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: testBlock, Count: 1})

	servertest.PrepareGameMasterPlacement(c)

	c.PlaceBlockAt(4, 63, 5, protocol.FaceTop, 0.5)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("non-operator ack sequence = %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(4, 63, 5, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}
	if got := srv.BlockAt(4, 64, 5); got != 0 {
		t.Errorf("world after a non-operator's test block placement = %d, want 0 (air)", got)
	}
}

// TestACreativeNonOperatorCannotBreakAGameMasterBlock is the break half of
// the gate: a player in creative without the gamemaster permission cannot
// break any of the gamemaster blocks, so the server keeps each block and
// resends it, which is what reverts the break the breaker's client predicted.
// vanilla: ServerPlayerGameMode.destroyBlock, GameMasterBlock
func TestACreativeNonOperatorCannotBreakAGameMasterBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Guest")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	for _, name := range []string{
		"minecraft:command_block",
		"minecraft:chain_command_block",
		"minecraft:repeating_command_block",
		"minecraft:structure_block",
		"minecraft:jigsaw",
		"minecraft:test_block",
		"minecraft:test_instance_block",
	} {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("no %s state", name)
		}
		if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], state) {
			t.Fatalf("could not stand the %s", name)
		}
		c.Settle()

		c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
		if seq := c.ExpectBlockAck(); seq != 1 {
			t.Errorf("%s: break acknowledged as sequence %d, want 1", name, seq)
		}
		got := c.ExpectBlockUpdate()
		if got.X != servertest.DigTarget[0] || got.Y != servertest.DigTarget[1] || got.Z != servertest.DigTarget[2] || got.State != state {
			t.Errorf("%s: refusal resent (%d,%d,%d) state %d, want the block's own (%d,%d,%d) state %d",
				name, got.X, got.Y, got.Z, got.State, servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], state)
		}
		if after := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]); after != state {
			t.Errorf("%s: block after a non-operator's break = %d, want %d", name, after, state)
		}
	}
}

// TestARefusedGameMasterBreakResendsTheBlockEntity checks the refused break
// of a structure block goes back with its block entity's data: the
// breaker's client dropped the entity with its predicted break, and the
// state alone would bring the block back blank.
// vanilla: ServerPlayerGameMode.destroyBlock (level.sendBlockUpdated)
func TestARefusedGameMasterBreakResendsTheBlockEntity(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Guest")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]
	srv.RunLine(fmt.Sprintf(`setblock %d %d %d minecraft:structure_block{name:"minecraft:outline",sizeX:5}`, x, y, z))
	// The setblock's own update and data go out at a tick's flush, ahead
	// of the dig.
	servertest.RunTicks(t, srv, 1)
	c.Settle()

	// The refusal's resend rides the next flush.
	c.StartDigging(x, y, z)
	var updated bool
	var entity []byte
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		r := protocol.NewReader(data)
		px, py, pz := r.ReadPosition()
		if px != x || py != y || pz != z {
			continue
		}
		switch id {
		case protocol.IDPlayBlockUpdate:
			updated = true
		case protocol.IDPlayBlockEntityData:
			if !updated {
				t.Error("the block entity data came before the block update, which a client drops")
			}
			entity = data
		}
		if entity != nil {
			break
		}
	}
	if !updated {
		t.Fatal("the refused break resent no block update")
	}
	if entity == nil {
		t.Fatal("the refused break resent the state without the structure block's data")
	}
	r := protocol.NewReader(entity)
	r.ReadPosition()
	if got, want := r.ReadVarInt(), world.BlockEntityTypeID("minecraft:structure_block"); got != want {
		t.Errorf("block entity data type = %d, want %d (the structure block)", got, want)
	}
	tag, _, err := nbt.DecodeNetwork(r.Rest())
	if err != nil {
		t.Fatalf("block entity data NBT: %v", err)
	}
	compound, _ := tag.(nbt.Compound)
	if got, _ := nbt.Get[nbt.String](compound, "name"); got != "minecraft:outline" {
		t.Errorf("resent structure name = %q, want minecraft:outline", got)
	}
}

// TestACreativeOperatorBreaksAGameMasterBlock is the other half: the same
// break by a creative operator goes through, the block becoming air for the
// world and the watcher.
func TestACreativeOperatorBreaksAGameMasterBlock(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	state, ok := registry.FindState("minecraft:command_block", nil)
	if !ok {
		t.Fatal("no command block state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], state) {
		t.Fatal("could not stand the command block")
	}
	actor.MoveToGround(0.5, 64, 0.5, true)
	actor.Settle()
	watcher.Settle()

	actor.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])

	if got := watcher.ExpectBlockUpdate(); got.X != servertest.DigTarget[0] || got.Y != servertest.DigTarget[1] ||
		got.Z != servertest.DigTarget[2] || got.State != 0 {
		t.Errorf("operator's break told the watcher (%d,%d,%d) state %d, want (%d,%d,%d) air",
			got.X, got.Y, got.Z, got.State, servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("operator's break acknowledged as sequence %d, want 1", seq)
	}
	servertest.ExpectBlockNamed(t, srv, servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], "minecraft:air", nil)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestAnAdventureOperatorCannotLayAGameMasterBlock is the mode half: an
// operator holds a command block whose minecraft:can_place_on names the
// clicked block, which is what opens an adventure hand's placement, and the
// gamemaster gate refuses it still because adventure is not creative.
func TestAnAdventureOperatorCannotLayAGameMasterBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "adventure"
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))

	stack := servertest.AdventuredStack(t, "minecraft:command_block", protocol.DataComponentCanPlaceOn, "minecraft:stone")
	srv.GiveSlot("Actor", player.HotbarSlot, stack)

	servertest.PrepareGameMasterPlacement(c)

	c.PlaceBlockAt(4, 63, 5, protocol.FaceTop, 0.5)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("adventure operator ack sequence = %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(4, 63, 5, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}
	if got := srv.BlockAt(4, 64, 5); got != 0 {
		t.Errorf("world after an adventure operator's placement = %d, want 0 (air)", got)
	}
}
