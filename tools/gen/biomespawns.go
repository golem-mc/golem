package main

import (
	"path"
	"sort"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "biomespawns",
		doc:  "internal/server/entity/spawning_gen.go: each biome's spawner lists",
		run:  runBiomeSpawns,
	})
}

type spawner struct {
	Type                       string
	Weight, MinCount, MaxCount int
}

type spawnCost struct {
	Type           string
	Charge, Budget float64
}

// biomeSpawnCosts reads the spawn_costs of the natural_mob_spawns overlay's
// argument, sorted by entity id.
// vanilla: MobSpawnSettings.MobSpawnCost.CODEC
func biomeSpawnCosts(b obj, where string) []spawnCost {
	attrs, _ := b.obj("attributes")
	attr, _ := attrs.obj("minecraft:gameplay/natural_mob_spawns")
	settings, _ := attr.obj("argument")
	costs, _ := settings.obj("spawn_costs")
	var out []spawnCost
	for typ, raw := range costs {
		c := asObj(raw, "a spawn cost")
		if !keysAre(c, "charge", "energy_budget") {
			fail("%s: unsupported spawn cost %v", where, c)
		}
		out = append(out, spawnCost{typ, c.num("charge"), c.num("energy_budget")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// biomeSpawners reads the natural_mob_spawns environment attribute overlay.
// vanilla: MobSpawnSettings.CODEC, MobSpawnSettings.SpawnerData.CODEC
func biomeSpawners(b obj, where string) map[string][]spawner {
	attrs, _ := b.obj("attributes")
	attr, ok := attrs.obj("minecraft:gameplay/natural_mob_spawns")
	if !ok || attr.str("modifier") != "overlay" {
		fail("%s: missing or unsupported natural_mob_spawns overlay", where)
	}
	settings, _ := attr.obj("argument")
	spawns, _ := settings.obj("spawns_by_category")
	if spawns == nil {
		fail("%s: missing spawns_by_category", where)
	}
	positive := func(o obj, key string) int {
		n := o.num(key)
		if n < 1 || n > 2147483647 || n != float64(int64(n)) {
			fail("%s: %s is not a positive int: %v", where, key, n)
		}
		return int(n)
	}
	out := map[string][]spawner{}
	for category := range spawns {
		for _, raw := range spawns.list(category) {
			s := asObj(raw, "a spawner")
			entry := spawner{Type: s.str("type"), Weight: positive(s, "weight")}
			if _, constant := asNum(s["count"]); constant {
				entry.MinCount = positive(s, "count")
				entry.MaxCount = entry.MinCount
			} else {
				count := asObj(s.field("count"), "a spawn count")
				if count.str("type") != "minecraft:uniform" || !keysAre(count, "type", "min_inclusive", "max_inclusive") {
					fail("%s: unsupported spawn count provider %v", where, count)
				}
				entry.MinCount, entry.MaxCount = positive(count, "min_inclusive"), positive(count, "max_inclusive")
			}
			if entry.MinCount > entry.MaxCount {
				fail("%s: inverted spawn count range %+v", where, entry)
			}
			out[category] = append(out[category], entry)
		}
	}
	return out
}

// Each biome's spawns_by_category carries the monster, creature, water_creature,
// underground_water_creature and water_ambient lists, kept whole and in file
// order because the weighted pick runs over the whole list.
var spawnTables = []struct{ variable, doc, key string }{
	{"biomeMonsterSpawns", "holds the monster category's half of the biome spawn\n" +
		"// settings. The lists are whole, unsimulated types included: a roll landing on\n" +
		"// one spends the attempt, keeping vanilla's share of the dice.", "monster"},
	{"biomeCreatureSpawns", "holds the creature category's half of the biome spawn\n" +
		"// settings, whole as the monster lists are.", "creature"},
	{"biomeWaterSpawns", "holds the water creature category's half of the biome spawn\n" +
		"// settings, whole as the monster lists are: the unsimulated types stay in.", "water_creature"},
	{"biomeWaterAmbientSpawns", "holds the water ambient category's half of the biome spawn\n" +
		"// settings, whole as the monster lists are: the unsimulated fish stay in.", "water_ambient"},
	{"biomeUndergroundWaterSpawns", "holds the underground water creature category's half of\n" +
		"// the biome spawn settings.", "underground_water_creature"},
	{"biomeAxolotlSpawns", "holds the axolotls category of the biome spawn settings.", "axolotls"},
	{"biomeAmbientSpawns", "holds the ambient category's half of the biome spawn settings.", "ambient"},
}

func runBiomeSpawns(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	const dir = "minecraft/worldgen/biome"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	registered, err := syncedEntries(e, "minecraft:worldgen/biome")
	if err != nil {
		return err
	}
	consts, err := idents("Biome", registered)
	if err != nil {
		return err
	}
	constOf := make(map[string]string, len(registered))
	for i, n := range registered {
		constOf[n] = consts[i]
	}
	type biome struct {
		name, ident string
		spawners    map[string][]spawner
		costs       []spawnCost
	}
	biomes := make([]biome, 0, len(files))
	for _, rel := range files {
		b, err := dpRead(dp, path.Join(dir, rel))
		if err != nil {
			return err
		}
		name := "minecraft:" + strings.TrimSuffix(rel, ".json")
		ident, ok := constOf[name]
		if !ok {
			fail("%s is not in the biome registry", name)
		}
		def := decode(b, rel)
		biomes = append(biomes, biome{name, ident, biomeSpawners(def, rel), biomeSpawnCosts(def, rel)})
	}

	f := e.file("entity", "by tools/gen biomespawns from the biome definitions of the vanilla {version} data pack")
	f.WriteString("import \"github.com/golem-mc/golem/registry\"\n\n")
	for i, t := range spawnTables {
		if i > 0 {
			f.WriteString("\n")
		}
		f.Printf("// %s %s\n", t.variable, t.doc)
		f.Printf("// vanilla: data/minecraft/worldgen/biome/*.json, \"spawners\" → \"%s\"\n", t.key)
		f.Printf("var %s = [registry.BiomeCount][]spawnData{\n", t.variable)
		for _, b := range biomes {
			entries := b.spawners[t.key]
			if len(entries) == 0 {
				continue
			}
			f.Printf("\tregistry.%s: {\n", b.ident)
			for _, s := range entries {
				typ, err := ident("EntityType", s.Type)
				if err != nil {
					return err
				}
				f.Printf("\t\t{registry.%s, %d, %d, %d},\n", typ, s.Weight, s.MinCount, s.MaxCount)
			}
			f.WriteString("\t},\n")
		}
		f.WriteString("}\n")
	}
	f.WriteString("\n// biomeSpawnCosts holds each biome's spawn_costs: the charge a mob of the type adds\n" +
		"// and the energy budget a new one must stay under.\n" +
		"// vanilla: MobSpawnSettings.mobSpawnCosts\n")
	f.WriteString("var biomeSpawnCosts = [registry.BiomeCount][]spawnCost{\n")
	for _, b := range biomes {
		if len(b.costs) == 0 {
			continue
		}
		f.Printf("\tregistry.%s: {\n", b.ident)
		for _, c := range b.costs {
			typ, err := ident("EntityType", c.Type)
			if err != nil {
				return err
			}
			f.Printf("\t\t{registry.%s, %v, %v},\n", typ, c.Charge, c.Budget)
		}
		f.WriteString("\t},\n")
	}
	f.WriteString("}\n")
	costed := map[string]bool{}
	var costedOrder []string
	for _, b := range biomes {
		for _, c := range b.costs {
			if !costed[c.Type] {
				costed[c.Type] = true
				costedOrder = append(costedOrder, c.Type)
			}
		}
	}
	f.WriteString("\n// spawnCostTypes marks the mob types some biome gives a spawn cost.\n" +
		"// vanilla: MobSpawnSettings.mobSpawnCosts\n")
	f.WriteString("var spawnCostTypes = [registry.EntityTypeCount]bool{\n")
	for _, name := range costedOrder {
		typ, err := ident("EntityType", name)
		if err != nil {
			return err
		}
		f.Printf("\tregistry.%s: true,\n", typ)
	}
	f.WriteString("}\n")
	e.logf("biomes: %d", len(biomes))
	return e.emit("internal/server/entity/spawning_gen.go", f)
}
