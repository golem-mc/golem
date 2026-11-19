package gamemode

// The gamemaster block tests: the command block, structure block, jigsaw and
// the two test block items are vanilla's GameMasterBlockItem, which refuses
// placement unless the placer can use the gamemaster blocks - creative mode
// and the gamemaster permission - while the block itself would stand for
// anyone who got one.

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
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
