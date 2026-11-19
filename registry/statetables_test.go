package registry

import (
	"strconv"
	"testing"
)

// TestStatePropTablesMatchMaps checks every typed accessor against the
// property map it was built from, for every state.
func TestStatePropTablesMatchMaps(t *testing.T) {
	sides := [4]string{"north", "south", "east", "west"}
	for id := range StateCount {
		s := uint16(id)
		props := StateProps(s)
		want, ok := facingOrder[props["facing"]]
		if !ok {
			want = facingOrder["east"]
		}
		if got := StateFacing(s); got != int(want) {
			t.Fatalf("state %d facing = %d, want %d", id, got, want)
		}
		pw, _ := strconv.Atoi(props["power"])
		if got := StatePower(s); got != pw {
			t.Fatalf("state %d power = %d, want %d", id, got, pw)
		}
		if got := StatePowered(s); got != (props["powered"] == "true") {
			t.Fatalf("state %d powered = %v", id, got)
		}
		for i, side := range sides {
			want := WireSideNone
			switch props[side] {
			case "side":
				want = WireSideSide
			case "up":
				want = WireSideUp
			}
			if got := StateWireSide(s, i); got != want {
				t.Fatalf("state %d %s = %d, want %d", id, side, got, want)
			}
		}
	}
}

func TestFacingIndex(t *testing.T) {
	for name, want := range map[string]int{"down": 0, "up": 1, "north": 2, "south": 3, "west": 4, "east": 5, "": 5, "sideways": 5} {
		if got := FacingIndex(name); got != want {
			t.Errorf("FacingIndex(%q) = %d, want %d", name, got, want)
		}
	}
}
