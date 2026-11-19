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
		name: "tagdefs",
		doc:  "registry/tagdefs_gen.go: the vanilla tags as their files define them",
		run:  runTagdefs,
	})
}

// tagRegistries are the directories under tags/ of the registries whose tags
// a data pack can change.
var tagRegistries = []string{
	"block", "item", "entity_type", "fluid", "enchantment", "banner_pattern",
	"instrument", "worldgen/biome", "timeline", "damage_type", "dialog",
	"painting_variant",
}

// registries_gen.go holds the tags flattened; a data pack that changes one tag
// has to change every tag that references it, so ApplyPackTags resolves these
// unflattened definitions together with the pack's. An entry that references
// another tag keeps its leading '#'. An optional entry (an object with
// "required": false) stops the generation: no vanilla tag has one.
func runTagdefs(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	f := e.file("registry", "by tools/gen tagdefs from the {version} data pack")
	f.WriteString(`// tagDefs holds the vanilla tags as their files define them, by registry and
// tag name: an entry starting with '#' references another tag of the registry.
var tagDefs = map[string]map[string][]string{
`)
	total := 0
	for _, reg := range tagRegistries {
		dir := path.Join("minecraft/tags", reg)
		files, err := vanilla.Files(dp, dir, ".json")
		if err != nil {
			return err
		}
		tags := map[string][]string{}
		for _, rel := range files {
			var tag struct {
				Values []any `json:"values"`
			}
			if err := vanilla.ReadJSON(dp, path.Join(dir, rel), &tag); err != nil {
				return err
			}
			entries := make([]string, 0, len(tag.Values))
			for _, v := range tag.Values {
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("%s: unsupported entry %v", path.Join(dir, rel), v)
				}
				entries = append(entries, s)
			}
			tags["minecraft:"+strings.TrimSuffix(rel, ".json")] = entries
		}
		names := sortedKeys(tags)
		f.Printf("\t\"minecraft:%s\": {\n", reg)
		for _, name := range names {
			quoted := make([]string, len(tags[name]))
			for i, entry := range tags[name] {
				quoted[i] = gogen.Quote(entry)
			}
			f.Printf("\t\t%s: {%s},\n", gogen.Quote(name), strings.Join(quoted, ", "))
		}
		f.WriteString("\t},\n")
		total += len(names)
	}
	f.WriteString("}\n")
	if err := e.emit("registry/tagdefs_gen.go", f); err != nil {
		return err
	}
	e.logf("tags: %d in %d registries", total, len(tagRegistries))
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
