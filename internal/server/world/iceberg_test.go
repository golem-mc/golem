package world

import "testing"

func TestOverworldIcebergsInFrozenOcean(t *testing.T) {
	t.Parallel()
	g := NewOverworldGenerator(5)
	attempts := map[string]int{}
	for i := range g.placed {
		f := &g.placed[i]
		name := f.feature.Name
		if name != "minecraft:iceberg_packed" && name != "minecraft:iceberg_blue" {
			continue
		}
		attempts[name] = 0
		place := f.place
		f.place = func(r *decorRegion, p [3]int32) { attempts[name]++; place(r, p) }
	}
	if len(attempts) != 2 {
		t.Fatalf("compiled iceberg kinds = %d, want 2", len(attempts))
	}
	// The assertion needs one placement attempt passing the biome filter.
	for x := int32(-92); x <= -84 && attempts["minecraft:iceberg_packed"] == 0; x++ {
		for z := int32(-84); z <= -76 && attempts["minecraft:iceberg_packed"] == 0; z++ {
			g.Generate(newChunk(x, z))
		}
	}
	t.Logf("iceberg attempts in seed 5's frozen ocean: %v", attempts)
	if attempts["minecraft:iceberg_packed"] == 0 {
		t.Fatal("no iceberg passed the frozen-ocean biome filter")
	}
}
