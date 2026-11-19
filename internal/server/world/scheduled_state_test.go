package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestScheduledWritesPreserveUnchangedProperties(t *testing.T) {
	for _, tc := range []struct {
		name, block   string
		before, after map[string]string
		prepare       func(*World)
		visit         func(*Physics, uint16, *[]BlockChange)
	}{
		{"leaf", "minecraft:oak_leaves", map[string]string{"distance": "7", "persistent": "true", "waterlogged": "true"}, map[string]string{"distance": "1", "persistent": "true", "waterlogged": "true"},
			func(w *World) { w.SetBlock(9, GroundLevel, 8, registry.BlockOakLog.DefaultState()) },
			func(p *Physics, s uint16, changes *[]BlockChange) { p.leafTick(8, GroundLevel, 8, s, changes) }},
		{"scaffolding", "minecraft:scaffolding", map[string]string{"distance": "5", "bottom": "true", "waterlogged": "true"}, map[string]string{"distance": "0", "bottom": "false", "waterlogged": "true"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) {
				var falls []FallingSpawn
				p.scaffoldingTick(8, GroundLevel, 8, s, changes, &falls)
			}},
		{"wet ghast", "minecraft:dried_ghast", map[string]string{"hydration": "1", "facing": "west", "waterlogged": "true"}, map[string]string{"hydration": "2", "facing": "west", "waterlogged": "true"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) { p.driedGhastTick(8, GroundLevel, 8, s, changes) }},
		{"dry ghast", "minecraft:dried_ghast", map[string]string{"hydration": "2", "facing": "east"}, map[string]string{"hydration": "1", "facing": "east"}, nil,
			func(p *Physics, s uint16, changes *[]BlockChange) { p.driedGhastTick(8, GroundLevel, 8, s, changes) }},
		{"egg", "minecraft:turtle_egg", map[string]string{"hatch": "2", "eggs": "4"}, map[string]string{"hatch": "2", "eggs": "3"}, nil,
			func(p *Physics, _ uint16, _ *[]BlockChange) {
				if _, ok := p.TurtleEggTrampled(8, GroundLevel, 8, 1); !ok {
					t.Fatal("egg was not trampled")
				}
			}},
		{"rail", "minecraft:powered_rail", map[string]string{"waterlogged": "true", "powered": "true"}, map[string]string{"waterlogged": "true", "powered": "true", "shape": "east_west"},
			func(w *World) {
				w.SetBlock(9, GroundLevel, 8, registry.BlockPoweredRail.DefaultState())
				w.SetBlock(8, GroundLevel, 9, registry.BlockRedstoneBlock.DefaultState())
			},
			func(p *Physics, s uint16, changes *[]BlockChange) {
				r, _ := p.railAround(8, GroundLevel, 8, s)
				r.updateDir(true, false, changes)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			t.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			if tc.prepare != nil {
				tc.prepare(w)
			}
			state := registry.MustState(tc.block, tc.before)
			w.SetBlock(8, GroundLevel, 8, state)
			var changes []BlockChange
			tc.visit(p, state, &changes)
			if got, want := w.BlockAt(8, GroundLevel, 8), registry.MustState(tc.block, tc.after); got != want {
				t.Fatalf("got %v, want %v", registry.StateProps(got), registry.StateProps(want))
			}
		})
	}
}
