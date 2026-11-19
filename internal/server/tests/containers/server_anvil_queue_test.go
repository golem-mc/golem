package containers

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnAnvilTakesBlockUpdateAndEventFollowThePacketsQueuedBeforeThem takes a
// rename out of a pristine anvil on a roll that chips it while the taker's
// mailbox is held with an item's spawn queued: the use event and the chip's
// Block Update are queued behind that spawn, not written past it. The event
// goes out at once and the block update with the tick's flush.
// vanilla: AnvilMenu.onTake (Level.levelEvent, Level.setBlock),
// ChunkHolder.broadcastChanges
func TestAnAnvilTakesBlockUpdateAndEventFollowThePacketsQueuedBeforeThem(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})
	renamed := sword
	name := protocol.Text{Text: "Sharp Sword"}
	renamed.CustomName = &name
	w.RenameInAnvil("Sharp Sword", renamed, 1)

	// A seed whose first roll wears the anvil (below 12%).
	seed := int64(0)
	for rand.New(rand.NewSource(seed)).Float64() >= 0.12 {
		seed++
	}
	srv.SetLevelSeed(seed)

	release := srv.HoldMailbox("Testificate")
	defer release()
	srv.SpawnItemEntity(4.5, 70, 0.5, "minecraft:stone", 1)
	servertest.AwaitTicks(t, srv, 3)

	// The click's own corrections are written straight, so they pass the
	// hold; the answer to a ping would wait behind it, which is why the
	// click is not waited on until the release.
	w.Click(2, 0, protocol.ClickModePickup)
	servertest.AwaitTicks(t, srv, 3)
	release()

	var order []int32
	for id := range w.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlaySpawnEntity, protocol.IDPlayBlockUpdate, protocol.IDPlayWorldEvent:
			order = append(order, id)
		}
		if id == protocol.IDPlayBlockUpdate && slices.Contains(order, protocol.IDPlayWorldEvent) {
			break
		}
	}
	want := []int32{protocol.IDPlaySpawnEntity, protocol.IDPlayWorldEvent, protocol.IDPlayBlockUpdate}
	if !slices.Equal(order, want) {
		t.Errorf("packet order = %v, want the spawn, the event, then the chip's block update %v", order, want)
	}
}
