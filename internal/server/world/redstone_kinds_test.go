package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSignalKindsCoverEveryState(t *testing.T) {
	n := 0
	for id := uint16(0); ; id++ {
		name, _, ok := registry.BlockStateName(id)
		if !ok {
			break
		}
		n++
		if got, want := signalKindOf(id), signalKindOfName(name); got != want {
			t.Fatalf("state %d (%s): kind %d, want %d", id, name, got, want)
		}
	}
	if len(signalKinds) != n {
		t.Fatalf("table has %d entries, registry has %d states", len(signalKinds), n)
	}
	for name, want := range map[string]signalKind{
		"minecraft:redstone_wire": kindWire, "minecraft:lever": kindLever,
		"minecraft:stone_button": kindButton, "minecraft:stone": kindOther,
	} {
		id, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("no state for %s", name)
		}
		if got := signalKindOf(id); got != want {
			t.Errorf("%s: kind %d, want %d", name, got, want)
		}
	}
	if signalKindOf(65535) != kindOther {
		t.Error("an ID past the palette must read as kindOther")
	}
}
