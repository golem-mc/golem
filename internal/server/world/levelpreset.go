package world

import (
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

// overworldPreset is the world preset of a noise overworld: the normal one,
// one of the two that read other noise settings with the same biome source,
// or single_biome_surface, the normal settings with plains everywhere.
// vanilla: WorldPresets.Bootstrap.registerOverworlds
type overworldPreset uint8

const (
	presetNormal overworldPreset = iota
	presetLargeBiomes
	presetAmplified
	presetSingleBiome
)

const (
	levelTypeNormal      = "minecraft:normal"
	levelTypeFlat        = "minecraft:flat"
	levelTypeLargeBiomes = "minecraft:large_biomes"
	levelTypeAmplified   = "minecraft:amplified"
	levelTypeSingleBiome = "minecraft:single_biome_surface"
)

// parseLevelType returns the world preset a server.properties level-type
// names: lowercased, with the minecraft namespace when it has none. A preset
// this server does not generate (largebiomes and default among them, as in
// vanilla) logs vanilla's warning and gives minecraft:normal.
// vanilla: DedicatedServerProperties.WorldDimensionData.create
func parseLevelType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	id := value
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	switch id {
	case levelTypeNormal, levelTypeFlat, levelTypeLargeBiomes, levelTypeAmplified, levelTypeSingleBiome, levelTypeDebug:
		return id
	}
	slog.Warn("Failed to parse level-type " + value + ", defaulting to " + levelTypeNormal)
	return levelTypeNormal
}

// presetOf returns the overworld preset of a level-type or noise settings name
// (the two share the names); any other name is the normal overworld.
func presetOf(name string) overworldPreset {
	switch name {
	case levelTypeLargeBiomes:
		return presetLargeBiomes
	case levelTypeAmplified:
		return presetAmplified
	case levelTypeSingleBiome:
		return presetSingleBiome
	}
	return presetNormal
}

// settings is the preset's noise settings name in world_gen_settings.dat.
func (p overworldPreset) settings() string {
	switch p {
	case presetLargeBiomes:
		return levelTypeLargeBiomes
	case presetAmplified:
		return levelTypeAmplified
	}
	return "minecraft:overworld"
}

// dimension is the terrain noise the preset's settings build.
// vanilla: NoiseGeneratorSettings.overworld
func (p overworldPreset) dimension() terrain.Dimension {
	switch p {
	case presetLargeBiomes:
		return terrain.OverworldLarge
	case presetAmplified:
		return terrain.OverworldAmplified
	}
	return terrain.Overworld
}
