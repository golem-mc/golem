package datapack

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LoadResources reads every data/<namespace>/<dir>/**/*.json of the packs
// (dir e.g. "loot_table") by resource id, a later pack's file replacing the
// whole of an earlier one's.
// vanilla: SimpleJsonResourceReloadListener.scanDirectory
func LoadResources(packs []string, dir string, skip func(file string, err error)) (map[string][]byte, error) {
	out := map[string][]byte{}
	filters := LoadFilters(packs)
	for i, pack := range packs {
		dropHidden(out, filters[i], dir+"/", ".json")
		if err := withPack(pack, func(fsys fs.FS) error { return loadPackResources(fsys, dir, out, skip) }); err != nil {
			return nil, fmt.Errorf("data pack %s: %w", filepath.Base(pack), err)
		}
	}
	return out, nil
}

func loadPackResources(pack fs.FS, dir string, out map[string][]byte, skip func(file string, err error)) error {
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
		base := path.Join("data", ns.Name(), dir)
		err := fs.WalkDir(pack, base, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && file == base {
					return fs.SkipDir
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(file, ".json") {
				return nil
			}
			id := strings.TrimSuffix(strings.TrimPrefix(file, base+"/"), ".json")
			if !validNamespace(ns.Name()) || !validPath(id) {
				skip(file, fmt.Errorf("invalid id %s:%s", ns.Name(), id))
				return nil
			}
			data, err := fs.ReadFile(pack, file)
			if err != nil {
				return err
			}
			out[ns.Name()+":"+id] = data
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
