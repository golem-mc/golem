package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestPlayerEquipmentSendsBodyAndSaddle checks that the Set Equipment packet
// carries the body and saddle stacks under slot ordinals 6 and 7.
func TestPlayerEquipmentSendsBodyAndSaddle(t *testing.T) {
	t.Parallel()
	armor, _ := registry.ItemID("minecraft:leather_horse_armor")
	saddle, _ := registry.ItemID("minecraft:saddle")
	p := &player.Player{EntityID: 7}
	p.Inventory(func(inv *player.Inventory) {
		inv.Body = protocol.ItemStack{ItemID: armor, Count: 1}
		inv.Saddle = protocol.ItemStack{ItemID: saddle, Count: 1}
	})

	b := protocol.NewReader(server.PlayerEquipmentForTest(p))
	if id := b.ReadVarInt(); id != 7 {
		t.Fatalf("entity id = %d, want 7", id)
	}
	for i := range 8 {
		entry := b.ReadU8()
		wantEntry := uint8(i)
		if i < 7 {
			wantEntry |= 0x80
		}
		if entry != wantEntry {
			t.Fatalf("entry %d slot byte = %#x, want %#x", i, entry, wantEntry)
		}
		stack, ok := b.ReadSlot()
		if !ok {
			t.Fatalf("entry %d unreadable", i)
		}
		want := int32(0)
		switch i {
		case 6:
			want = armor
		case 7:
			want = saddle
		}
		if want != 0 && stack.ItemID != want || want == 0 && !stack.Empty() {
			t.Errorf("entry %d = %+v, want item %d", i, stack, want)
		}
	}
}
