package terrain

import (
	_ "embed"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"strconv"
	"strings"
)

//go:embed overworld_features.txt
var overworldFeaturesData string

// OverworldFeature is a placed feature of the overworld's decoration with the
// biomes whose feature lists carry it, as a bit set indexed by OverworldBiome.
type OverworldFeature struct {
	Name   string
	Slot   levelgen.FeatureSlot
	Biomes uint64
}

// OverworldFeatures are the placed features FeatureSorter orders from the
// feature lists of the overworld's biomes, taken in the parameter list's
// order, by step and then index. They are read from the file the vanilla
// server's own FeatureSorter wrote (tools/java/OverworldFeatureOrder.java).
// vanilla: FeatureSorter.buildFeaturesPerStep, MultiNoiseBiomeSource
var OverworldFeatures = parseOverworldFeatures(overworldFeaturesData)

func parseOverworldFeatures(data string) []OverworldFeature {
	byName := make(map[string]OverworldBiome, OverworldBiomeCount)
	for i, name := range overworldBiomeNames {
		byName[name] = OverworldBiome(i)
	}
	var out []OverworldFeature
	for line := range strings.SplitSeq(data, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "feature" {
			continue
		}
		step, errStep := strconv.Atoi(f[1])
		index, errIndex := strconv.Atoi(f[2])
		if errStep != nil || errIndex != nil {
			panic("overworld_features.txt: bad line " + line)
		}
		feature := OverworldFeature{Name: f[3], Slot: levelgen.FeatureSlot{Step: int32(step), Index: int32(index)}}
		for name := range strings.SplitSeq(f[4], ",") {
			b, ok := byName[name]
			if !ok {
				panic("overworld_features.txt: unknown biome " + name)
			}
			feature.Biomes |= 1 << b
		}
		out = append(out, feature)
	}
	return out
}
