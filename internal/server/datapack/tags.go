package datapack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// tagExt is the suffix of a tag file.
// vanilla: FileToIdConverter.json
const tagExt = ".json"

// TagEntry is one value of a function tag: a function or, with Tag, another
// function tag. A missing Required entry keeps the whole tag from loading.
// vanilla: TagEntry
type TagEntry struct {
	ID       string
	Tag      bool
	Required bool
}

// FunctionTags are the function tags of a world's data packs: each tag's id
// and its entries, the packs layered.
type FunctionTags map[string][]TagEntry

// tagFile is the JSON of a tag file.
// vanilla: TagFile.CODEC
type tagFile struct {
	Replace bool              `json:"replace"`
	Values  []json.RawMessage `json:"values"`
}

// RawTag is one tag after the packs are layered: Replace is set when a pack
// discarded what the packs before it, and so the built-in tags, defined.
type RawTag struct {
	Replace bool
	Entries []TagEntry
}

// LoadFunctionTags reads every data/<namespace>/tags/function/**/*.json of
// the packs in layer order, a later pack's entries appended unless it says
// replace. A file that does not parse is reported through skip.
// vanilla: TagLoader.load
func LoadFunctionTags(packs []string, skip func(file string, err error)) (FunctionTags, error) {
	raw, err := LoadTags(packs, "function", skip)
	if err != nil {
		return nil, err
	}
	out := FunctionTags{}
	for id, tag := range raw {
		out[id] = tag.Entries
	}
	return out, nil
}

// LoadTags reads data/<namespace>/tags/<dir>/**/*.json (dir e.g. "block"),
// layered like LoadFunctionTags.
// vanilla: TagLoader.load
func LoadTags(packs []string, dir string, skip func(file string, err error)) (map[string]RawTag, error) {
	out := map[string]RawTag{}
	for _, pack := range packs {
		if err := withPack(pack, func(fsys fs.FS) error { return loadPackTags(fsys, dir, out, skip) }); err != nil {
			return nil, fmt.Errorf("data pack %s: %w", filepath.Base(pack), err)
		}
	}
	return out, nil
}

func loadPackTags(pack fs.FS, dir string, out map[string]RawTag, skip func(file string, err error)) error {
	namespaces, err := fs.ReadDir(pack, "data")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		if !ns.IsDir() {
			continue
		}
		base := path.Join("data", ns.Name(), "tags", dir)
		err := fs.WalkDir(pack, base, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && file == base {
					return fs.SkipDir
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(file, tagExt) {
				return nil
			}
			tagPath := strings.TrimSuffix(strings.TrimPrefix(file, base+"/"), tagExt)
			if !validNamespace(ns.Name()) || !validPath(tagPath) {
				skip(file, fmt.Errorf("invalid tag id %s:%s", ns.Name(), tagPath))
				return nil
			}
			data, err := fs.ReadFile(pack, file)
			if err != nil {
				return err
			}
			entries, replace, err := parseTagFile(data)
			if err != nil {
				skip(file, err)
				return nil
			}
			id := ns.Name() + ":" + tagPath
			tag := out[id]
			if replace {
				tag = RawTag{Replace: true}
			}
			tag.Entries = append(tag.Entries, entries...)
			out[id] = tag
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// parseTagFile reads a tag file's values: strings or {id, required} objects.
// vanilla: TagEntry.FULL_CODEC
func parseTagFile(data []byte) ([]TagEntry, bool, error) {
	var f tagFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false, err
	}
	entries := make([]TagEntry, 0, len(f.Values))
	for _, raw := range f.Values {
		var text string
		required := true
		if err := json.Unmarshal(raw, &text); err != nil {
			var obj struct {
				ID       string `json:"id"`
				Required *bool  `json:"required"`
			}
			if err := json.Unmarshal(raw, &obj); err != nil || obj.ID == "" {
				return nil, false, fmt.Errorf("invalid tag entry %s", raw)
			}
			text = obj.ID
			if obj.Required != nil {
				required = *obj.Required
			}
		}
		tag := strings.HasPrefix(text, "#")
		text = strings.TrimPrefix(text, "#")
		ns, p, found := strings.Cut(text, ":")
		if !found {
			ns, p = "minecraft", text
		}
		if !validNamespace(ns) || !validPath(p) || p == "" {
			return nil, false, fmt.Errorf("invalid tag entry %s", raw)
		}
		entries = append(entries, TagEntry{ID: ns + ":" + p, Tag: tag, Required: required})
	}
	return entries, f.Replace, nil
}
