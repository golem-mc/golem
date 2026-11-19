package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

func TestHandbackPlacesBackTheWayVanillaDoes(t *testing.T) {
	log := func(n int32) protocol.ItemStack {
		return protocol.ItemStack{ItemID: itemID(t, "minecraft:oak_log"), Count: n}
	}
	inv := &player.Inventory{Selected: 2}
	inv.Set(player.HotbarSlot, log(10))
	inv.Set(player.HotbarSlot+2, log(60))
	inv.Set(player.OffHandSlot, log(60))
	inv.Set(1, log(20))
	s := &Session{Carried: log(5)}

	back := s.Close(View{Kind: KindPlayer}, inv, true, false)

	// The held slot first, the off hand next, then the hotbar in order.
	want := map[int]protocol.ItemStack{
		player.HotbarSlot + 2: log(64),
		player.OffHandSlot:    log(64),
		player.HotbarSlot:     log(27),
		1:                     {},
	}
	for slot, stack := range want {
		if got := inv.Slots[slot]; !got.Equal(stack) {
			t.Errorf("slot %d = %+v, want %+v", slot, got, stack)
		}
	}
	if !s.Carried.Empty() || len(back.Dropped) != 0 || !back.Visible {
		t.Errorf("cursor %+v, dropped %+v, visible %v; want empty, none, true", s.Carried, back.Dropped, back.Visible)
	}

	// Nothing partial: the first empty hotbar slot, never the empty off hand.
	inv = &player.Inventory{}
	grid := []protocol.ItemStack{log(70)}
	(&Session{}).Close(View{Kind: KindCrafting, Grid: grid}, inv, false, false)
	if got := inv.Slots[player.HotbarSlot]; !got.Equal(log(64)) {
		t.Errorf("first hotbar slot = %+v, want 64 logs", got)
	}
	if got := inv.Slots[player.HotbarSlot+1]; !got.Equal(log(6)) {
		t.Errorf("second hotbar slot = %+v, want 6 logs", got)
	}
	if !inv.Slots[player.OffHandSlot].Empty() {
		t.Errorf("off hand = %+v, want empty", inv.Slots[player.OffHandSlot])
	}

	// A player leaving the world drops everything.
	inv = &player.Inventory{}
	inv.Set(2, log(3))
	back = (&Session{}).Close(View{Kind: KindPlayer}, inv, true, true)
	if len(back.Dropped) != 1 || !back.Dropped[0].Equal(log(3)) || len(back.Slots) != 0 || !inv.Slots[2].Empty() {
		t.Errorf("dropped %+v, slots %v, grid cell %+v; want the 3 logs dropped", back.Dropped, back.Slots, inv.Slots[2])
	}
}
