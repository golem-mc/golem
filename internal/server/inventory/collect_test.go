package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The double-click gather (mode 6) as ordinary unit tests, driving a
// Session over a chest the way the server hands one to each click.

// TestCollectFillsFromPartialStacksFirst checks the sweep's two passes: a
// full stack sitting ahead of a partial one is only opened for the
// remainder, since the first pass skips the stacks already full.
func TestCollectFillsFromPartialStacksFirst(t *testing.T) {
	stone := itemID(t, "minecraft:stone")
	full := protocol.ItemStack{ItemID: stone, Count: 64}
	partial := protocol.ItemStack{ItemID: stone, Count: 30}

	chest := make(slots, 27)
	chest[0] = full
	chest[1] = partial
	view := View{Kind: KindChest, Entity: chest}
	inv := &player.Inventory{}
	session := &Session{Carried: protocol.ItemStack{ItemID: stone, Count: 10}}
	var outcome Outcome

	session.Click(view, inv, protocol.ClickModeCollect, 0, 5, false, 0, &outcome)

	if got := chest[0]; got.Count != 40 {
		t.Errorf("chest slot 0 = %+v, want the forty the full stack was left with", got)
	}
	if got := chest[1]; !got.Empty() {
		t.Errorf("chest slot 1 = %+v, want it drained into the cursor", got)
	}
	if got := session.Carried.Count; got != 64 {
		t.Errorf("carried = %d, want the whole stack the gather filled", got)
	}
}
