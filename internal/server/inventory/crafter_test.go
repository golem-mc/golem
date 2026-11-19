package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestCrafterPourOneAtATimeIntoAOneItemContainer checks that a stack above
// the container's slot limit is poured an item at a time: a jukebox-sized
// slot keeps one and the rest is handed back.
func TestCrafterPourOneAtATimeIntoAOneItemContainer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind Kind
		item string
	}{
		{KindJukebox, "minecraft:music_disc_13"},
		{KindChiseledBookshelf, "minecraft:book"},
	} {
		dest := &testBlock{kind: tc.kind, slots: make([]protocol.ItemStack, 1)}
		left, _ := CrafterPour(dest, item(t, tc.item, 9), protocol.FaceTop)
		if left.Count != 8 || dest.slots[0].Count != 1 {
			t.Errorf("%s: %d items came back and %d went in, want 8 and 1", tc.item, left.Count, dest.slots[0].Count)
		}
	}
}
