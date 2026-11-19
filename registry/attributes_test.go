package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAttributeTable checks the hand-kept attribute registry: IDs dense and
// in order, every default inside its range, and the IDs protocol names
// agreeing with it.
func TestAttributeTable(t *testing.T) {
	for i, a := range Attributes {
		if a.ID != int32(i) {
			t.Errorf("%s has ID %d at index %d", a.Name, a.ID, i)
		}
		if a.Default < a.Min || a.Default > a.Max {
			t.Errorf("%s default %v outside [%v, %v]", a.Name, a.Default, a.Min, a.Max)
		}
	}
	for name, id := range map[string]int32{
		"minecraft:max_health":     protocol.AttributeMaxHealth,
		"minecraft:movement_speed": protocol.AttributeMovementSpeed,
	} {
		if a, ok := AttributeByName(name); !ok || a.ID != id {
			t.Errorf("%s = %+v, want ID %d", name, a, id)
		}
	}
}

// TestItemModifiersFeedTheLookups checks the lookups the combat rules read
// against the modifiers they are summed from.
func TestItemModifiersFeedTheLookups(t *testing.T) {
	item := func(name string) int32 {
		id, ok := ItemID(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		return id
	}
	if p, tough := ArmorOf(item("minecraft:netherite_chestplate")); p != 8 || tough != 3 {
		t.Errorf("netherite chestplate armour = %v/%v, want 8/3", p, tough)
	}
	sword := item("minecraft:diamond_sword")
	if got := AttackDamage(sword); got != 6 {
		t.Errorf("diamond sword damage = %v, want 6", got)
	}
	if got := AttackSpeed(sword); got != float32(-2.4) {
		t.Errorf("diamond sword speed = %v, want -2.4", got)
	}
	if p, _ := ArmorOf(sword); p != 0 {
		t.Errorf("a sword's armour = %v, want none", p)
	}
}

func TestSlotGroupHolds(t *testing.T) {
	for _, c := range []struct {
		group, slot string
		want        bool
	}{
		{"any", "feet", true},
		{"hand", "offhand", true},
		{"hand", "head", false},
		{"armor", "body", true},
		{"armor", "mainhand", false},
		{"chest", "chest", true},
		{"chest", "legs", false},
	} {
		if got := SlotGroupHolds(c.group, c.slot); got != c.want {
			t.Errorf("SlotGroupHolds(%q, %q) = %v, want %v", c.group, c.slot, got, c.want)
		}
	}
}
