package server

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// loadPackRegistries applies the enabled packs' entries of the synchronized
// registries. It runs once per start: vanilla loads world registries with the
// world, not on /reload or /datapack. The table is process-wide, so a server
// without pack entries leaves it alone.
// vanilla: WorldLoader.load, RegistryDataLoader.WORLD_REGISTRIES
func loadPackRegistries(dirs []string, skip func(file string, err error)) error {
	// Decoded before anything is applied: an enchantment vanilla rejects fails
	// the load with no pack registry entry published.
	enchantments, err := loadPackEnchantments(dirs, skip)
	if err != nil {
		return err
	}
	packs := map[string]map[string]nbt.Tag{}
	for _, reg := range registry.Synchronized {
		entries, err := datapack.LoadRegistryEntries(dirs, strings.TrimPrefix(reg.Name, "minecraft:"), skip)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			packs[reg.Name] = entries
		}
	}
	if err := loadPackDamageTypes(dirs, skip); err != nil {
		return err
	}
	if err := loadPackTrialSpawners(dirs, skip); err != nil {
		return err
	}
	if err := loadPackEnchantmentProviders(dirs, skip); err != nil {
		return err
	}
	if err := loadPackSulfurArchetypes(dirs, skip); err != nil {
		return err
	}
	unloaded, err := datapack.UnloadedWorldgen(dirs)
	if err != nil {
		return err
	}
	for _, pack := range unloaded {
		slog.Warn("Data pack " + pack + " has world generation files, which are not loaded")
	}
	if len(packs) == 0 {
		registry.ApplyPackEnchantments(enchantments)
		return nil
	}
	for _, problem := range registry.ApplyPackRegistries(packs) {
		slog.Warn("Data pack registry entry skipped: " + problem)
	}
	if err := loadPackBiomes(dirs, skip); err != nil {
		return err
	}
	registry.ApplyPackEnchantments(enchantments)
	return nil
}

// loadPackJigsaw hands the packs' structure, structure set, template pool and
// processor list files, and their templates, to world generation.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES
func loadPackJigsaw(levelDir string, dirs []string, skip func(file string, err error)) error {
	var p world.PackWorldgen
	for dir, out := range map[string]*map[string][]byte{
		"worldgen/structure": &p.Structures, "worldgen/structure_set": &p.StructureSets,
		"worldgen/template_pool": &p.TemplatePools, "worldgen/processor_list": &p.ProcessorLists,
	} {
		files, err := datapack.LoadResources(dirs, dir, skip)
		if err != nil {
			return err
		}
		*out = files
	}
	templates := &datapack.TemplateFiles{WorldDir: levelDir}
	templates.SetPacks(dirs)
	p.Template = func(id string) ([]byte, bool) {
		data, ok, err := templates.ReadTemplate(id)
		return data, ok && err == nil
	}
	world.ApplyPackWorldgen(p)
	return nil
}

// loadPackBiomes decodes overridden built-in biome files into the climate and
// natural spawning override tables. New biomes are ignored here because
// ApplyPackRegistries already logged and skipped them; a file that fails
// decoding keeps the built-in biome's climate and spawns.
// vanilla: Biome.DIRECT_CODEC, MobSpawnSettings.CODEC
func loadPackBiomes(dirs []string, skip func(file string, err error)) error {
	files, err := datapack.LoadResources(dirs, "worldgen/biome", skip)
	if err != nil {
		return err
	}
	climates := make(map[registry.Biome]registry.BiomeClimate, len(files))
	spawns := make(map[registry.Biome]datapack.BiomeOverride, len(files))
	for name, data := range files {
		biome, ok := registry.BiomeByName(name)
		if !ok {
			continue
		}
		ov, err := datapack.DecodeBiome(data)
		if err != nil {
			skip(name, err)
			continue
		}
		climates[biome] = ov.Climate
		spawns[biome] = ov
	}
	registry.ApplyPackBiomeClimates(climates)
	entity.ApplyPackBiomeSpawns(spawns)
	return nil
}

// loadPackEnchantments decodes the packs' enchantment files into the
// definitions the rules read, over the built-in ones. What the tree cannot
// represent is left out with a Warn naming the enchantment. A file vanilla's
// codec rejects fails world loading, naming it.
// vanilla: Enchantment.DIRECT_CODEC
func loadPackEnchantments(dirs []string, skip func(file string, err error)) (map[string]registry.EnchantmentDef, error) {
	files, err := datapack.LoadResources(dirs, "enchantment", skip)
	if err != nil {
		return nil, err
	}
	predicates, err := datapack.LoadResources(dirs, "predicate", skip)
	if err != nil {
		return nil, err
	}
	predicate := func(name string) ([]byte, bool) {
		data, ok := predicates[name]
		return data, ok
	}
	defs := make(map[string]registry.EnchantmentDef, len(files))
	for name, data := range files {
		def, dropped, err := datapack.DecodeEnchantmentWith(name, data, predicate)
		if err != nil {
			if errors.Is(err, datapack.ErrUnsupported) {
				skip(name, err)
				continue
			}
			return nil, fmt.Errorf("enchantment %s: %w", name, err)
		}
		for _, d := range dropped {
			slog.Warn("Enchantment " + name + " does not support " + d)
		}
		defs[name] = def
	}
	return defs, nil
}

// loadPackDamageTypes layers the packs' damage types over the built-in table
// the death messages, scaling and exhaustion read.
func loadPackDamageTypes(dirs []string, skip func(file string, err error)) error {
	raw, err := datapack.LoadResources(dirs, "damage_type", skip)
	if err != nil {
		return err
	}
	types := make(map[string]registry.DamageTypeInfo, len(raw))
	for id, data := range raw {
		t, err := datapack.DecodeDamageType(data)
		if err != nil {
			skip(id, err)
			continue
		}
		types[id] = t
	}
	registry.ApplyPackDamageTypes(types)
	return nil
}

// loadPackTrialSpawners layers the packs' trial_spawner configs over the
// built-in ones a spawner names by registry key.
func loadPackTrialSpawners(dirs []string, skip func(file string, err error)) error {
	entries, err := datapack.LoadRegistryEntries(dirs, "trial_spawner", skip)
	if err != nil {
		return err
	}
	for _, name := range entity.ApplyPackTrialSpawnerConfigs(entries) {
		slog.Warn("Trial spawner config " + name + " skipped: not a valid configuration")
	}
	return nil
}

// loadPackEnchantmentProviders layers the packs' enchantment providers over
// the built-in table the mob equipment, raid and enderman rolls read.
func loadPackEnchantmentProviders(dirs []string, skip func(file string, err error)) error {
	raw, err := datapack.LoadResources(dirs, "enchantment_provider", skip)
	if err != nil {
		return err
	}
	providers := make(map[string]registry.EnchantmentProvider, len(raw))
	for id, data := range raw {
		p, err := datapack.DecodeEnchantmentProvider(data)
		if err != nil {
			skip(id, err)
			continue
		}
		providers[id] = p
	}
	registry.ApplyPackEnchantmentProviders(providers)
	return nil
}

// loadPackSulfurArchetypes layers the packs' sulfur cube archetypes over the
// built-in list the cubes' behaviour reads. It runs after the damage types,
// which a contact damage names.
func loadPackSulfurArchetypes(dirs []string, skip func(file string, err error)) error {
	raw, err := datapack.LoadResources(dirs, "sulfur_cube_archetype", skip)
	if err != nil {
		return err
	}
	archetypes := make(map[string]registry.SulfurArchetype, len(raw))
	for id, data := range raw {
		a, err := datapack.DecodeSulfurArchetype(data)
		if err != nil {
			skip(id, err)
			continue
		}
		archetypes[id] = a
	}
	registry.ApplySulfurArchetypes(archetypes)
	return nil
}
