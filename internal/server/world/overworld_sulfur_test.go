package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestOverworldSulfurCavesBenchmarkFeatures checks that the benchmark reaches
// successful template and lake placement, beyond their failed attempts.
func TestOverworldSulfurCavesBenchmarkFeatures(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(5)
	g.Generate(newChunk(1000, 1000))
	// Both features write potent sulfur; count around each call to distinguish
	// their output from one another and from the terrain's sulfur and cinnabar.
	count := func(r *decorRegion) int {
		n := 0
		for _, proto := range r.protos {
			if proto == nil {
				continue
			}
			for _, section := range proto.blocks {
				if section == nil {
					continue
				}
				for _, s := range section {
					if s != 0 && registry.BlockOf(s-1) == registry.BlockPotentSulfur {
						n++
					}
				}
			}
		}
		return n
	}
	placed := map[string]bool{
		"minecraft:rooted_sulfur_spring": false,
		"minecraft:sulfur_pool":          false,
	}
	for i := range g.placed {
		f := &g.placed[i]
		name, place := f.feature.Name, f.place
		if _, ok := placed[name]; !ok {
			continue
		}
		f.place = func(r *decorRegion, p [3]int32) {
			if placed[name] {
				place(r, p)
				return
			}
			before := count(r)
			place(r, p)
			if count(r) > before {
				placed[name] = true
				t.Logf("%s placed from %v", name, p)
			}
		}
	}
	// One successful placement of each feature proves the benchmark reaches
	// both paths; later chunks cannot add coverage to these assertions.
	for i := int32(0); i < 81 && (!placed["minecraft:rooted_sulfur_spring"] || !placed["minecraft:sulfur_pool"]); i++ {
		g.Generate(newChunk(-20+i%9, -25+i/9%9))
	}
	for name, ok := range placed {
		if !ok {
			t.Errorf("%s placed no potent sulfur in the benchmark window", name)
		}
	}
}
