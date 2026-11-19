package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestWearLeavesUnwearablesAlone is the other half of the rule: no wear on
// an empty hand, on an item with no max damage of its own, and none either
// when the cause costs nothing.
func TestWearLeavesUnwearablesAlone(t *testing.T) {
	stone := protocol.ItemStack{ItemID: itemID(t, "minecraft:stone"), Count: 1}
	if worn, broke := Wear(stone, 5); broke || !worn.Equal(stone) {
		t.Errorf("wear on a plain stone came to %+v (broke %v)", worn, broke)
	}
	if worn, broke := Wear(protocol.ItemStack{}, 5); broke || !worn.Empty() {
		t.Errorf("wear on an empty hand came to %+v (broke %v)", worn, broke)
	}
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_sword"), Count: 1, Damage: 10}
	if worn, broke := Wear(sword, 0); broke || worn.Damage != 10 {
		t.Errorf("a wear of nothing changed the stack to %+v (broke %v)", worn, broke)
	}

	// A stack cannot carry negative wear, whatever a caller computes.
	sword.Damage = -3
	if worn, broke := Wear(sword, 1); broke || worn.Damage != -3 {
		t.Errorf("negative wear came to %+v (broke %v)", worn, broke)
	}

	// The unbreakable component spares a stack whose max damage is otherwise
	// there: the wear never lands.
	unbreakable := protocol.ItemStack{ItemID: itemID(t, "minecraft:iron_sword"), Count: 1, Unbreakable: true}
	if worn, broke := Wear(unbreakable, 1); broke || !worn.Equal(unbreakable) {
		t.Errorf("wear on an unbreakable sword came to %+v (broke %v)", worn, broke)
	}
}

// TestBreakStatusBySlot pins which slot of the inventory breaks with which
// animation: each hand and each armour piece its own status, and any other
// slot none at all, those being the only places wear lands.
func TestBreakStatusBySlot(t *testing.T) {
	cases := []struct {
		slot   int
		status uint8
	}{
		{player.HotbarSlot, protocol.EntityStatusBreakMainHand},
		{player.HotbarSlot + 8, protocol.EntityStatusBreakMainHand},
		{player.OffHandSlot, protocol.EntityStatusBreakOffHand},
		{player.ArmorSlot, protocol.EntityStatusBreakHead},
		{player.ArmorSlot + 1, protocol.EntityStatusBreakChest},
		{player.ArmorSlot + 2, protocol.EntityStatusBreakLegs},
		{player.ArmorSlot + 3, protocol.EntityStatusBreakFeet},
	}
	for _, c := range cases {
		status, ok := BreakStatus(c.slot)
		if !ok || status != c.status {
			t.Errorf("BreakStatus(%d) = (%v, %v), want (%v, true)", c.slot, status, ok, c.status)
		}
	}
	if _, ok := BreakStatus(20); ok {
		t.Error("a plain inventory slot has a break status, want none")
	}
}
