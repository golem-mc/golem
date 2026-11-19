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
