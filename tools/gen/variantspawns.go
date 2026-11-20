package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "variantspawns",
		doc:  "registry/variantspawns_gen.go: built-in spawn_conditions of the seven variant registries",
		run:  runVariantSpawns,
	})
}

var variantSpawnRegistries = []string{
	"minecraft:cat_variant",
	"minecraft:chicken_variant",
	"minecraft:cow_variant",
	"minecraft:frog_variant",
	"minecraft:pig_variant",
	"minecraft:wolf_variant",
	"minecraft:zombie_nautilus_variant",
}

func runVariantSpawns(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	var body strings.Builder
	body.WriteString("// builtinVariantSpawns holds the vanilla data pack's spawn_conditions for\n")
	body.WriteString("// each of the seven variant registries, by registry name then entry name.\n")
	body.WriteString("var builtinVariantSpawns = map[string]map[string][]VariantSpawnSelector{\n")

	total := 0
	for _, regName := range variantSpawnRegistries {
		dir := "minecraft/" + strings.TrimPrefix(regName, "minecraft:")
		files, err := vanilla.Files(dp, dir, ".json")
		if err != nil {
			return err
		}
		if err := needAtLeast(len(files), 2, regName); err != nil {
			return err
		}
		fmt.Fprintf(&body, "\t%s: {\n", gogen.Quote(regName))
		for _, name := range files {
			file := path.Join(dir, name)
			entry := "minecraft:" + strings.TrimSuffix(name, ".json")
			doc := decode(mustRead(dp, file), file)
			conds := doc.list("spawn_conditions")
			fmt.Fprintf(&body, "\t\t%s: {\n", gogen.Quote(entry))
			for _, raw := range conds {
				sel := asObj(raw, file)
				if !keysWithin(sel, "priority", "condition") {
					fail("%s: unmodelled spawn_conditions selector fields %v", file, sortedKeys(sel))
				}
				prio, ok := asNum(sel["priority"])
				if !ok {
					fail("%s: selector priority is not a number", file)
				}
				fmt.Fprintf(&body, "\t\t\t%s,\n", formatVariantSelector(int(prio), sel, file))
			}
			body.WriteString("\t\t},\n")
			total++
		}
		body.WriteString("\t},\n")
	}
	body.WriteString("}\n")

	if err := needAtLeast(total, 30, "variant spawn entries"); err != nil {
		return err
	}
	e.logf("wrote %d variant spawn entries across %d registries", total, len(variantSpawnRegistries))
	f := e.file("registry", "by tools/gen variantspawns from a vanilla {version} server")
	f.WriteString(body.String())
	return e.emit("registry/variantspawns_gen.go", f)
}

func formatVariantSelector(prio int, sel obj, file string) string {
	cond, hasCond := sel.obj("condition")
	if !hasCond {
		return fmt.Sprintf("{Priority: %d, Kind: SpawnConditionAlways}", prio)
	}
	typ := cond.str("type")
	switch typ {
	case "minecraft:biome":
		if !keysWithin(cond, "type", "biomes") {
			fail("%s: unmodelled biome condition fields %v", file, sortedKeys(cond))
		}
		tag, ids := readHolderSet(cond["biomes"], file)
		if tag != "" {
			return fmt.Sprintf("{Priority: %d, Kind: SpawnConditionBiome, BiomeTag: %s}", prio, gogen.Quote(tag))
		}
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = "mustBiome(" + gogen.Quote(id) + ")"
		}
		return fmt.Sprintf("{Priority: %d, Kind: SpawnConditionBiome, Biomes: []Biome{%s}}", prio, strings.Join(parts, ", "))
	case "minecraft:moon_brightness":
		if !keysWithin(cond, "type", "range") {
			fail("%s: unmodelled moon_brightness condition fields %v", file, sortedKeys(cond))
		}
		hasMin, minVal, hasMax, maxVal := readDoubleRange(cond["range"], file)
		var fields []string
		fields = append(fields, fmt.Sprintf("Priority: %d", prio), "Kind: SpawnConditionMoonBrightness")
		if hasMin {
			fields = append(fields, "HasMinMoon: true", "MinMoon: "+gogen.Float(minVal))
		}
		if hasMax {
			fields = append(fields, "HasMaxMoon: true", "MaxMoon: "+gogen.Float(maxVal))
		}
		return "{" + strings.Join(fields, ", ") + "}"
	case "minecraft:structure":
		if !keysWithin(cond, "type", "structures") {
			fail("%s: unmodelled structure condition fields %v", file, sortedKeys(cond))
		}
		tag, ids := readHolderSet(cond["structures"], file)
		if tag != "" {
			return fmt.Sprintf("{Priority: %d, Kind: SpawnConditionStructure, StructureTag: %s}", prio, gogen.Quote(tag))
		}
		return fmt.Sprintf("{Priority: %d, Kind: SpawnConditionStructure, Structures: []string{%s}}", prio, quotedList(ids))
	default:
		fail("%s: unknown spawn condition type %q", file, typ)
		return ""
	}
}

func readHolderSet(v any, file string) (tag string, ids []string) {
	switch x := v.(type) {
	case string:
		if t, ok := strings.CutPrefix(x, "#"); ok {
			return t, nil
		}
		return "", []string{x}
	case []any:
		ids = make([]string, 0, len(x))
		for _, item := range x {
			s := asStr(item, file)
			if strings.HasPrefix(s, "#") {
				fail("%s: holder set list may not contain tag %q", file, s)
			}
			ids = append(ids, s)
		}
		slices.Sort(ids)
		return "", ids
	default:
		fail("%s: holder set is not a string or list: %T", file, v)
		return "", nil
	}
}

func readDoubleRange(v any, file string) (hasMin bool, minVal float64, hasMax bool, maxVal float64) {
	if n, ok := asNum(v); ok {
		return true, n, true, n
	}
	o := asObj(v, file)
	if !keysWithin(o, "min", "max") {
		fail("%s: unmodelled range fields %v", file, sortedKeys(o))
	}
	if o.has("min") {
		hasMin, minVal = true, o.num("min")
	}
	if o.has("max") {
		hasMax, maxVal = true, o.num("max")
	}
	return hasMin, minVal, hasMax, maxVal
}
