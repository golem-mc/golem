package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestJukeboxTransfersUseTheStacksSong(t *testing.T) {
	t.Parallel()
	stick := item(t, "minecraft:stick", 1)
	stick.SetJukeboxPlayable(registry.JukeboxSongID("minecraft:cat"))
	disc := item(t, "minecraft:music_disc_13", 1)
	disc.Remove(protocol.DataComponentJukeboxPlayable)
	for _, tc := range []struct {
		stack protocol.ItemStack
		want  bool
	}{{stick, true}, {disc, false}} {
		box := &testBlock{kind: KindJukebox, slots: make([]protocol.ItemStack, 1)}
		if got := CanPlaceItem(box, tc.stack, 0); got != tc.want {
			t.Errorf("CanPlaceItem = %v, want %v", got, tc.want)
		}
		hopper := &testBlock{kind: KindHopper, slots: []protocol.ItemStack{tc.stack}}
		if moved, _ := Eject(hopper, box, protocol.FaceTop); moved != tc.want {
			t.Errorf("hopper moved = %v, want %v", moved, tc.want)
		}
		if tc.want && !box.slots[0].Equal(tc.stack) {
			t.Error("transfer lost the song component")
		}
	}
}
