package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestCauldronDousedEveryLevel(t *testing.T) {
	w, p := newPhysicsWorld(t)
	for _, block := range []registry.Block{registry.BlockWaterCauldron, registry.BlockPowderSnowCauldron} {
		for level := 1; level <= 3; level++ {
			state, _ := registry.WithInt(block.DefaultState(), registry.PropLevel, level)
			w.SetBlock(0, GroundLevel, 0, state)
			want := cauldronEmptyState
			if level > 1 {
				want = waterCauldronByLevel[level-1]
			}
			changes := p.CauldronDoused(0, GroundLevel, 0)
			expected := []BlockChange{{X: 0, Y: GroundLevel, Z: 0, State: want}}
			if !slices.Equal(changes, expected) || w.BlockAt(0, GroundLevel, 0) != want {
				t.Errorf("%s level %d: changes = %v, want %v", block.Name(), level, changes, expected)
			}
		}
	}
	for _, state := range []uint16{cauldronEmptyState, registry.BlockLavaCauldron.DefaultState(), registry.BlockStone.DefaultState()} {
		w.SetBlock(0, GroundLevel, 0, state)
		if changes := p.CauldronDoused(0, GroundLevel, 0); len(changes) != 0 || w.BlockAt(0, GroundLevel, 0) != state {
			t.Errorf("non-layered state %d changed: %v", state, changes)
		}
	}
}

var cauldronBenchChanges []BlockChange

func BenchmarkCauldronDoused(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	p := NewPhysics(w)
	state := registry.MustState("minecraft:powder_snow_cauldron", map[string]string{"level": "3"})
	b.ReportAllocs()
	for b.Loop() {
		w.SetBlock(8, GroundLevel, 8, state)
		cauldronBenchChanges = p.CauldronDoused(8, GroundLevel, 8)
	}
}
