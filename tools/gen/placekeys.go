package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "placekeys",
		doc:  "registry/placekeys_gen.go: the worldgen keys and structure templates /place answers for",
		run:  runPlaceKeys,
	})
}

// The /place command answers differently for a key the data pack names and a
// key it does not, so it needs the names but not the content.
var placeSections = []struct{ variable, dir, suffix, doc string }{
	{"FeatureKeys", "minecraft/worldgen/feature", ".json", "the features of the data pack"},
	{"StructureKeys", "minecraft/worldgen/structure", ".json", "the structures of the data pack"},
	{"TemplatePoolKeys", "minecraft/worldgen/template_pool", ".json", "the jigsaw template pools of the data pack"},
	{"BuiltinTemplates", "minecraft/structure", ".nbt", "the structure templates the server jar carries"},
}

func runPlaceKeys(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	f := e.file("registry", "by tools/gen placekeys from the vanilla {version} data pack")
	for _, s := range placeSections {
		files, err := vanilla.Files(dp, s.dir, s.suffix)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("placekeys: the data pack has no %s under %s", s.suffix, s.dir)
		}
		keys := make([]string, len(files))
		for i, rel := range files {
			keys[i] = "minecraft:" + strings.TrimSuffix(rel, s.suffix)
		}
		slices.Sort(keys)
		f.Printf("\n// %s holds the ids of %s.\n", s.variable, s.doc)
		f.Set(s.variable, keys)
		e.logf("%s: %d", s.variable, len(keys))
	}
	return e.emit("registry/placekeys_gen.go", f)
}
