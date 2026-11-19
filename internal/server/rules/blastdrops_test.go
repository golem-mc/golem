package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func blastItem(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
}

// TestBlastDropsMergeToSixteenAtTheFirstPosition checks that stacks of one
// item fill the first collector to 16, spill into the next, and keep the
// position of the first block that dropped into each.
func TestBlastDropsMergeToSixteenAtTheFirstPosition(t *testing.T) {
	cobble := blastItem(t, "minecraft:cobblestone")
	var d BlastDrops
	for i := range 40 {
		d.Add(protocol.ItemStack{ItemID: cobble, Count: 1}, [3]int32{int32(i), 0, 0})
	}
	got := d.Drops()
	if len(got) != 3 {
		t.Fatalf("%d stacks, want 3", len(got))
	}
	for i, want := range []struct {
		x     int32
		count int32
	}{{0, 16}, {16, 16}, {32, 8}} {
		if got[i].Stack.Count != want.count || got[i].Pos[0] != want.x {
			t.Errorf("stack %d = %d at x=%d, want %d at x=%d", i, got[i].Stack.Count, got[i].Pos[0], want.count, want.x)
		}
	}
}
