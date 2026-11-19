package datapack

import (
	"path/filepath"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// LoadRegistryEntries reads a synchronized registry's entries from
// data/<namespace>/<dir>/**/*.json of the packs (dir e.g. "painting_variant")
// by resource id, each file's JSON as the NBT the client decodes it from. A
// file that is not JSON is skipped.
// vanilla: RegistryDataLoader.loadContentsFromManager
func LoadRegistryEntries(packs []string, dir string, skip func(file string, err error)) (map[string]nbt.Tag, error) {
	raw, err := LoadResources(packs, dir, skip)
	if err != nil {
		return nil, err
	}
	out := make(map[string]nbt.Tag, len(raw))
	for id, data := range raw {
		tag, err := JSONTag(data)
		if err != nil {
			skip(id, err)
			continue
		}
		out[id] = tag
	}
	return out, nil
}

// UnloadedWorldgen names the packs that have JSON files under data/*/worldgen/
// other than worldgen/biome, which the server does not load.
func UnloadedWorldgen(packs []string) ([]string, error) {
	ignore := func(string, error) {}
	var out []string
	for _, pack := range packs {
		found := false
		ids, err := LoadResources([]string{pack}, "worldgen", ignore)
		if err != nil {
			return nil, err
		}
		for id := range ids {
			if !strings.HasPrefix(id[strings.IndexByte(id, ':')+1:], "biome/") {
				found = true
			}
		}
		if found {
			out = append(out, filepath.Base(pack))
		}
	}
	return out, nil
}
