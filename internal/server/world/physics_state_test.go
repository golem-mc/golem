package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPhysicsStateWritesKeepOtherProperties(t *testing.T) {
	for _, tc := range []struct {
		name          string
		block         string
		before, after map[string]string
		act           func(*Physics)
	}{
		{"waterlogged trapdoor", "minecraft:oak_trapdoor",
			map[string]string{"facing": "west", "half": "top", "waterlogged": "true"},
			map[string]string{"facing": "west", "half": "top", "waterlogged": "true", "open": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "") }},
		{"gate turns away from opener", "minecraft:oak_fence_gate",
			map[string]string{"facing": "north", "in_wall": "true"},
			map[string]string{"facing": "south", "in_wall": "true", "open": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "south") }},
		{"locked repeater wraps delay", "minecraft:repeater",
			map[string]string{"facing": "east", "delay": "4", "locked": "true"},
			map[string]string{"facing": "east", "delay": "1", "locked": "true"},
			func(p *Physics) { p.UseBlock(8, GroundLevel, 8, "") }},
		{"waterlogged dripleaf hit", "minecraft:big_dripleaf",
			map[string]string{"facing": "west", "waterlogged": "true"},
			map[string]string{"facing": "west", "waterlogged": "true", "tilt": "full"},
			func(p *Physics) { p.DripleafProjectiled(8, GroundLevel, 8) }},
		{"summoning waterlogged shrieker", "minecraft:sculk_shrieker",
			map[string]string{"can_summon": "true", "waterlogged": "true"},
			map[string]string{"can_summon": "true", "waterlogged": "true", "shrieking": "true"},
			func(p *Physics) { p.StartShriek(8, GroundLevel, 8, 3) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			w.SetBlock(8, GroundLevel, 8, registry.MustState(tc.block, tc.before))
			tc.act(p)
			if got, want := w.BlockAt(8, GroundLevel, 8), registry.MustState(tc.block, tc.after); got != want {
				t.Fatalf("state = %v, want %v", registry.StateProps(got), registry.StateProps(want))
			}
		})
	}
}
