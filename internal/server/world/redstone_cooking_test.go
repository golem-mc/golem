package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The comparator's read of the four containers this server added block
// entities for: the three cooking blocks and the brewing stand read out
// their fullness like a chest, and the crafter counts the slots that are
// disabled or hold anything.

// TestComparatorReadsCookingAndBrewingContainers reads the analog output of
// a blast furnace, a smoker and a brewing stand: each reads the fullness of
// its slots.
// vanilla: AbstractContainerMenu.getRedstoneSignalFromContainer
func TestComparatorReadsCookingAndBrewingContainers(t *testing.T) {
	w, p, clock := comparatorCircuit(t, "compare")
	dirt, _ := registry.ItemID("minecraft:dirt")
	// Any change beside the comparator books its two-tick visit, the way
	// the chest test wakes it.
	wake := func() {
		old := w.BlockAt(14, 63, 8)
		var next uint16
		if old == airState {
			next = rsState(t, "minecraft:cobblestone", nil)
		}
		if !w.SetBlock(14, 63, 8, next) {
			t.Fatalf("setup: the wake stone did not go in")
		}
		p.NotifyChanged(14, 63, 8, old)
		for tick := 1; tick <= 4; tick++ {
			*clock++
			p.Tick(*clock)
		}
	}

	cases := []struct {
		block  string
		kind   BlockEntityKind
		stacks int
		want   int
	}{
		{"minecraft:blast_furnace", KindBlastFurnace, 1, 5},
		{"minecraft:smoker", KindSmoker, 1, 5},
		{"minecraft:brewing_stand", KindBrewingStand, 2, 6},
	}
	for _, c := range cases {
		old := w.BlockAt(15, 63, 9)
		if !w.SetBlock(15, 63, 9, rsState(t, c.block, nil)) {
			t.Fatalf("setup: the %s did not go in", c.block)
		}
		p.NotifyChanged(15, 63, 9, old)
		be := w.EnsureBlockEntity(15, 63, 9, c.kind)
		be.Use(func(b *BlockEntity) {
			for slot := 0; slot < c.stacks; slot++ {
				b.Slots[slot] = protocol.ItemStack{ItemID: dirt, Count: 64}
			}
		})
		wake()
		if out := comparatorOutputAt(t, w, 14, 63, 9); out != c.want {
			t.Errorf("the comparator read %d off the %s, want %d", out, c.block, c.want)
		}
	}
}
