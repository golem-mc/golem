package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPartialPickupKeepsRemainderAndSkipsThrownItemTrigger: a stack that only
// partly fits is inserted as far as it goes and the client is told the slot,
// the rest stays on the ground, and neither the take animation, the
// picked-up stat nor the thrown-item criterion happens until the remainder
// is collected.
// vanilla: Inventory.add, ItemEntity.playerTouch, ServerPlayer.onItemPickup
func TestPartialPickupKeepsRemainderAndSkipsThrownItemTrigger(t *testing.T) {
	cfg, data := packConfig(t)
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	writePackFile(t, filepath.Join(data, "pk", "advancement", "thrown.json"),
		`{"criteria":{"got":{"trigger":"minecraft:thrown_item_picked_up_by_player","conditions":{"item":{"items":"minecraft:dirt"}}}}}`)
	srv, c := servertest.Join(t, cfg, "Packrat")
	servertest.HoldTicks(t, srv)

	stone := servertest.MustItemID(t, "minecraft:stone")
	dirt := servertest.MustItemID(t, "minecraft:dirt")
	for slot := 9; slot < 45; slot++ {
		srv.GiveSlot("Packrat", slot, protocol.ItemStack{ItemID: stone, Count: 64})
	}
	srv.GiveSlot("Packrat", 36, protocol.ItemStack{ItemID: dirt, Count: 63})
	c.Settle()

	srv.SpawnItemThrownBy("Packrat", 0.5, 64.125, 0.5, "minecraft:dirt", 2, 0)
	var slotSet, taken bool
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadVarInt()
			slot := r.ReadI16()
			stack, _ := r.ReadSlot()
			slotSet = slotSet || slot == 36 && stack.ItemID == dirt && stack.Count == 64
		case protocol.IDPlayPickupItem:
			taken = true
		}
		return false
	})
	if !slotSet {
		t.Error("the client was not sent the 64-dirt slot")
	}
	if taken {
		t.Error("a partial pickup played the take")
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Count != 1 {
		t.Errorf("items on the ground = %+v, want one dirt", items)
	}
	if got := srv.StatValue("Packrat", registry.StatPickedUp, "minecraft:dirt"); got != 0 {
		t.Errorf("picked_up stat = %d, want 0", got)
	}
	if srv.AdvancementObtained("Packrat", "pk:thrown", "got") {
		t.Error("a partial pickup earned the thrown-item criterion")
	}

	srv.GiveSlot("Packrat", 36, protocol.ItemStack{})
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if !srv.AdvancementObtained("Packrat", "pk:thrown", "got") {
		t.Error("collecting the remainder did not earn the thrown-item criterion")
	}
}
