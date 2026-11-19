package world

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// Decoration steps a flat world's settings adjust.
// vanilla: GenerationStep.Decoration
const (
	lakesStep                = 1
	topLayerModificationStep = 10
)

// flatLavaLakes are the LAKES features lakes adds, in order.
// vanilla: FlatLevelGeneratorSettings.createLakesList
var flatLavaLakes = []string{"minecraft:lake_lava_underground", "minecraft:lake_lava_surface"}

// overworldFeatures is the flat world's decoration before its fills: with
// lakes the lava lakes in LAKES, with features the biome's own features in
// every step but the structure steps (and LAKES when lakes replaces it). Each
// is slotted by its rank in its step's list and carries biome 0, the one biome
// a flat world's source answers. A biome outside the overworld's parameter
// list has no feature data here, so it decorates nothing.
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings, FeatureSorter.buildFeaturesPerStep
func (g *FlatGenerator) overworldFeatures() []terrain.OverworldFeature {
	decorate := g.features && (!g.void() || g.biomeName == "minecraft:the_void")
	biome, known := overworldBiomeNamed(g.biomeName)
	if decorate && !known {
		slog.Warn("This server does not generate the features of a flat world's biome outside the overworld", "biome", g.biomeName)
		decorate = false
	}
	var out []terrain.OverworldFeature
	var next [levelgen.DecorationSteps]int32
	add := func(name string, step int32) {
		out = append(out, terrain.OverworldFeature{Name: name, Slot: levelgen.FeatureSlot{Step: step, Index: next[step]}, Biomes: 1})
		next[step]++
	}
	for step := range int32(levelgen.DecorationSteps) {
		if g.lakes && step == lakesStep {
			for _, name := range flatLavaLakes {
				add(name, step)
			}
		}
		if !decorate || step == levelgen.UndergroundStructuresStep || step == levelgen.SurfaceStructuresStep || g.lakes && step == lakesStep {
			continue
		}
		for _, f := range terrain.OverworldFeatures {
			if f.Slot.Step == step && f.Biomes&(1<<biome) != 0 {
				add(f.Name, step)
			}
		}
	}
	return out
}

// void reports a flat world whose layers are all air, or that has none.
// vanilla: FlatLevelGeneratorSettings.updateLayers (voidGen)
func (g *FlatGenerator) void() bool {
	for _, l := range g.layers {
		if registry.BlockOf(l.state) != registry.BlockAir {
			return false
		}
	}
	return true
}

// overworldBiomeNamed is the overworld parameter list's biome of a biome id.
func overworldBiomeNamed(name string) (terrain.OverworldBiome, bool) {
	for b := range terrain.OverworldBiome(terrain.OverworldBiomeCount) {
		if name == "minecraft:"+b.Name() {
			return b, true
		}
	}
	return 0, false
}

// fills are the TOP_LAYER_MODIFICATION features that write each y of a layer
// outside the base, after the step's other features in placed: an air cell of
// the chunk at that y takes the layer's state. An air layer's fill writes
// nothing and is left out; it draws no randomness, so its slot changes no
// other feature.
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings, FillLayerFeature
func (g *FlatGenerator) fills(placed []terrain.OverworldFeature) []overworldPlaced {
	index := int32(0)
	for _, f := range placed {
		if f.Slot.Step == topLayerModificationStep {
			index++
		}
	}
	var out []overworldPlaced
	bottom := int32(worldMinY)
	for _, l := range g.layers {
		if inFlatBase(l.state) || overworldAirBlocks[registry.BlockOf(l.state)] {
			bottom += l.height
			continue
		}
		state := l.state
		for y := bottom; y < bottom+l.height && y < worldMinY+worldHeight; y++ {
			out = append(out, overworldPlaced{
				feature: terrain.OverworldFeature{Name: "minecraft:fill_layer", Slot: levelgen.FeatureSlot{Step: topLayerModificationStep, Index: index}, Biomes: 1},
				place: func(r *decorRegion, p [3]int32) {
					for dx := range int32(sectionSize) {
						for dz := range int32(sectionSize) {
							if overworldAirBlocks[registry.BlockOf(r.get(p[0]+dx, y, p[2]+dz))] {
								r.set(p[0]+dx, y, p[2]+dz, state)
							}
						}
					}
				},
			})
			index++
		}
		bottom += l.height
	}
	return out
}
