package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestOwnSignalIncludesUnconnectedDustWithoutBorrowingConductorPower(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	defer w.Close()
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	p := NewPhysics(w)
	// Elevated unconnected dust has no neighbor delivering a signal back.
	pos := [3]int32{8, 80, 8}
	for _, tc := range []struct {
		name  string
		props map[string]string
		want  bool
	}{
		{"minecraft:redstone_wire", map[string]string{"power": "7"}, true},
		{"minecraft:redstone_wire", map[string]string{"power": "0"}, false},
		{"minecraft:lever", map[string]string{"powered": "true"}, true},
		{"minecraft:lever", map[string]string{"powered": "false"}, false},
		{"minecraft:stone", nil, false},
	} {
		if !w.SetBlock(pos[0], pos[1], pos[2], registry.MustState(tc.name, tc.props)) {
			t.Fatal("unloaded test cell")
		}
		if p.HasNeighborSignal(pos[0], pos[1], pos[2]) {
			t.Fatal("fixture has neighbor power")
		}
		if got := p.HasOwnOrNeighborSignal(pos[0], pos[1], pos[2]); got != tc.want {
			t.Fatalf("%s %v own signal=%v", tc.name, tc.props, got)
		}
	}
	w.SetBlock(9, 80, 8, registry.MustState("minecraft:redstone_block", nil))
	if !p.HasOwnOrNeighborSignal(8, 80, 8) {
		t.Fatal("neighbor power ignored")
	}
}
