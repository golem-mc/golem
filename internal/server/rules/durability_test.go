package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestWearBreaksItemAtMaxDamage follows a sword's last two uses: one
// short of the end the wear lands on the stack, and the wear that reaches
// the max damage takes the whole item with it.
func TestWearBreaksItemAtMaxDamage(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_sword"), Count: 1}
	max := registry.MaxDamage(sword.ItemID)
	if max <= 0 {
		t.Fatalf("the iron sword takes no wear (max damage %v)", max)
	}

	// Two uses left: one wear lands, the stack told apart by it.
	sword.Damage = max - 2
	worn, broke := Wear(sword, 1)
	if broke || worn.Damage != max-1 {
		t.Errorf("one wear short of the end is %+v (broke %v), want the damage one below the max",
			worn, broke)
	}

	// The last use: the stack comes back empty, and broken.
	if worn, broke := Wear(worn, 1); !broke || !worn.Empty() {
		t.Errorf("the wear that reached the max left %+v (broke %v), want it gone", worn, broke)
	}

	// A heavy single wear jumps straight past the end.
	fresh, broke := Wear(protocol.ItemStack{ItemID: sword.ItemID, Count: 1, Damage: max - 1}, 5)
	if !broke || !fresh.Empty() {
		t.Errorf("a wear past the max left %+v (broke %v), want it gone", fresh, broke)
	}
}
