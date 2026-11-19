package registry

// BiomeClimate is the part of a biome's climate the rain reads: whether the
// biome precipitates at all, its base temperature before the height adjustment
// Biome.getHeightAdjustedTemperature applies, and whether the frozen
// temperature_modifier replaces that base in its cold patches.
type BiomeClimate struct {
	Precipitation bool
	Temperature   float32
	Frozen        bool
}

// Biome is a minecraft:worldgen/biome registry ID, the value a chunk stores.
type Biome int32

// NoBiome is the answer where no biome is known, such as an unloaded chunk.
const NoBiome Biome = -1

// Name returns the biome's registry name, or "" outside the registry.
func (b Biome) Name() string {
	if b < 0 || b >= BiomeCount {
		return ""
	}
	return biomeNames[b]
}

// BiomeByName returns the biome registered under name.
func BiomeByName(name string) (Biome, bool) {
	b, ok := biomeByName[name]
	if !ok {
		return NoBiome, false
	}
	return b, true
}

var biomeByName = func() map[string]Biome {
	m := make(map[string]Biome, BiomeCount)
	for i, n := range biomeNames {
		m[n] = Biome(i)
	}
	return m
}()

// Climate returns the biome's climate. Outside the registry it answers the
// vanilla biome defaults: precipitation at a temperate 0.5.
func (b Biome) Climate() BiomeClimate {
	if b < 0 || b >= BiomeCount {
		return BiomeClimate{Precipitation: true, Temperature: 0.5}
	}
	return biomeClimates[b]
}
