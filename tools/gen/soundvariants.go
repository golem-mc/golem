package main

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "soundvariants",
		doc:  "registry/soundvariants_gen.go: adult and baby sound sets of the five *_sound_variant registries",
		run:  runSoundVariants,
	})
}

var soundVariantRegistries = []string{
	"minecraft:cat_sound_variant",
	"minecraft:chicken_sound_variant",
	"minecraft:cow_sound_variant",
	"minecraft:pig_sound_variant",
	"minecraft:wolf_sound_variant",
}

// Inputs: each entry of the five registries is either flat (a cow's sounds) or
// has adult_sounds and baby_sounds; every value is a sound event name. A
// flat entry's sounds serve both ages. Any other shape stops the generation.
func runSoundVariants(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	var body strings.Builder
	body.WriteString("// builtinSoundVariants holds the vanilla data pack's sound sets by registry,\n")
	body.WriteString("// entry, then age (adult, baby), as sound key to sound event name.\n")
	body.WriteString("var builtinSoundVariants = map[string]map[string][2]map[string]string{\n")
	total := 0
	for _, regName := range soundVariantRegistries {
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
			doc := decode(mustRead(dp, file), file)
			adult, baby := doc, doc
			if doc.has("adult_sounds") {
				if !keysWithin(doc, "adult_sounds", "baby_sounds") {
					fail("%s: unmodelled fields %v", file, sortedKeys(doc))
				}
				adult, baby = asObj(doc["adult_sounds"], file), asObj(doc["baby_sounds"], file)
			}
			fmt.Fprintf(&body, "\t\t%s: {%s, %s},\n", gogen.Quote("minecraft:"+strings.TrimSuffix(name, ".json")),
				soundMap(adult, file), soundMap(baby, file))
			total++
		}
		body.WriteString("\t},\n")
	}
	body.WriteString("}\n")
	if err := needAtLeast(total, 15, "sound variant entries"); err != nil {
		return err
	}
	e.logf("wrote %d sound variant entries", total)
	f := e.file("registry", "by tools/gen soundvariants from a vanilla {version} server")
	f.WriteString(body.String())
	return e.emit("registry/soundvariants_gen.go", f)
}

func soundMap(o obj, file string) string {
	var parts []string
	for _, key := range slices.Sorted(maps.Keys(o)) {
		name, ok := o[key].(string)
		if !ok || !strings.HasSuffix(key, "_sound") {
			fail("%s: %s is not a sound name", file, key)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", gogen.Quote(key), gogen.Quote(name)))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
