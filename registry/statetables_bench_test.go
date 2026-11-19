package registry

import "testing"

func hopperStates() []uint16 {
	var ids []uint16
	for id := range stateByID {
		if name, _, ok := BlockStateName(uint16(id)); ok && name == "minecraft:hopper" {
			ids = append(ids, uint16(id))
		}
	}
	return ids
}

// BenchmarkHopperPropsParsed reads a hopper's enabled and facing properties
// by parsing the property string.
func BenchmarkHopperPropsParsed(b *testing.B) {
	ids := hopperStates()
	n := 0
	for i := range b.N {
		_, props, _ := BlockStateName(ids[i%len(ids)])
		p := ParseStateProps(props)
		if p["enabled"] == "true" || p["facing"] == "down" {
			n++
		}
	}
	_ = n
}

// BenchmarkHopperPropsTables reads the same properties from the typed tables.
func BenchmarkHopperPropsTables(b *testing.B) {
	ids := hopperStates()
	n := 0
	for i := range b.N {
		id := ids[i%len(ids)]
		if StateEnabled(id) || (StateHasFacing(id) && StateFacing(id) == 0) {
			n++
		}
	}
	_ = n
}

func TestHopperStateTables(t *testing.T) {
	ids := hopperStates()
	if len(ids) != 10 {
		t.Fatalf("hopper states = %d, want 10", len(ids))
	}
	for _, id := range ids {
		p := StateProps(id)
		if StateEnabled(id) != (p["enabled"] == "true") || !StateHasFacing(id) ||
			StateFacing(id) != int(facingOrder[p["facing"]]) {
			t.Errorf("state %d props %v disagree with tables", id, p)
		}
	}
}
