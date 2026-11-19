package registry

import (
	"strconv"
	"testing"
)

// TestStateValuesMatchMaps checks every typed read against the property map
// it was built from, for every state and Prop and a few IDs past the palette.
func TestStateValuesMatchMaps(t *testing.T) {
	for id := range StateCount + 3 {
		s := uint16(id)
		props := StateProps(s)
		for i := range stateTables.names {
			p := Prop(i)
			v, has := props[p.Name()]
			if got := StateValue(s, p); got != v {
				t.Fatalf("state %d %s = %q, want %q", id, p.Name(), got, v)
			}
			if got := StateHas(s, p); got != has {
				t.Fatalf("state %d has %s = %v, want %v", id, p.Name(), got, has)
			}
			if got := StateBool(s, p); got != (v == "true") {
				t.Fatalf("state %d %s bool = %v", id, p.Name(), got)
			}
			want, err := strconv.Atoi(v)
			if got, ok := StateInt(s, p); ok != (err == nil) || ok && got != want {
				t.Fatalf("state %d %s int = %d, %v; want %d, %v", id, p.Name(), got, ok, want, err)
			}
		}
	}
}

// TestStateValueTableSize reports the tables' size, so a property that
// splits many rows shows up.
func TestStateValueTableSize(t *testing.T) {
	rows := len(valueRows) / valueWidth
	bytes := len(stateValueRow)*2 + len(valueRows)
	t.Logf("%d props, %d rows, %d values, %d bytes", valueWidth, rows, len(valueNames), bytes)
	if bytes > 1<<20 {
		t.Errorf("state value tables take %d bytes, want under 1 MiB", bytes)
	}
}
