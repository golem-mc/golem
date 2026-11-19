package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestVibrationPathsFollowReloadedTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	w.SetBlock(4, GroundLevel, 1, registry.BlockGoldBlock.DefaultState())
	v := Vibration{X: 1.5, Y: float64(GroundLevel) + 0.5, Z: 1.5}
	dest := blockKey{5, GroundLevel, 1}
	check := func(want bool) {
		t.Helper()
		if got := VibrationOccluded(w, v.X, v.Y, v.Z, float64(dest.x), float64(dest.y), float64(dest.z)); got != want {
			t.Errorf("gold occludes vibration = %v, want %v", got, want)
		}
		p.effects = nil
		p.sculkResonate(dest, 1, uuid.Nil, uuid.Nil)
		if got := len(p.effects) == 2; got != want {
			t.Errorf("gold resonates = %v, want %v (effects: %v)", got, want, p.effects)
		}
	}
	check(false)
	entry := registry.PackTag{Replace: true, Entries: []registry.PackTagEntry{{ID: "minecraft:gold_block", Required: true}}}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:occludes_vibration_signals": entry,
		"minecraft:vibration_resonators":       entry,
	}})
	check(true)
	registry.ApplyPackTags(nil)
	check(false)
}
