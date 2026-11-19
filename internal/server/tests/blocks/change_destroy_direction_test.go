package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestChangeDestroyDirectionKeepsTheDig: re-aiming a survival dig neither
// breaks the block nor drops the held item, and the dig still finishes on STOP.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction CHANGE_DESTROY_DIRECTION
func TestChangeDestroyDirectionKeepsTheDig(t *testing.T) {
	t.Parallel()
	srv, miner, _, _ := crackScene(t)
	d := servertest.DigTarget
	held := srv.InventorySlot("Miner", player.HotbarSlot)

	miner.StartDigging(d[0], d[1], d[2])
	miner.ChangeDestroyDirection(d[0], d[1], d[2], protocol.FaceTop)
	miner.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.BlockAt(d[0], d[1], d[2]); got != servertest.BlockState(t, "minecraft:stone") {
		t.Fatalf("the block is state %d after the re-aim, want stone", got)
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot); got.ItemID != held.ItemID || got.Count != held.Count {
		t.Errorf("held slot = %+v after the re-aim, want %+v", got, held)
	}

	miner.StopDigging(d[0], d[1], d[2])
	miner.Settle()
	servertest.RunTicks(t, srv, 6)
	servertest.ExpectAir(t, srv, d)
	if got := srv.InventorySlot("Miner", player.HotbarSlot); got.ItemID != held.ItemID || got.Count != held.Count {
		t.Errorf("held slot = %+v after the dig, want %+v", got, held)
	}
}
