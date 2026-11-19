package registry

import "testing"

func TestPOIMembershipMatchesTypes(t *testing.T) {
	t.Parallel()
	for id := range 1 << 16 {
		state := uint16(id)
		typ := poiTypeOfBlock[BlockOf(state)]
		if typ == "minecraft:home" && StateValue(state, PropPart) != "head" {
			typ = ""
		}
		if got := IsPOI(state); got != (typ != "") {
			t.Fatalf("state %d: membership %t, want type %q", state, got, typ)
		}
		if got := POITypeOf(state); got != typ {
			t.Fatalf("state %d: type %q, want %q", state, got, typ)
		}
	}
}
