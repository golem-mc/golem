// Package datapack reads the world's data packs: the folders under
// <world>/datapacks and the function files they carry.
package datapack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Dir is the folder of a world that holds its data packs.
// vanilla: LevelResource.DATAPACK_DIR
const Dir = "datapacks"

// functionExt is the suffix of a function file.
// vanilla: ServerFunctionLibrary.LISTER
const functionExt = ".mcfunction"

// Functions are the function files of a world's data packs: each function's
// id ("namespace:path") and the lines of its file.
type Functions map[string][]string

// LoadFunctions reads every data/<namespace>/function/**/*.mcfunction of the
// packs in layer order, a later pack replacing the same id. A file name that
// is no valid id is skipped and reported through skip.
// vanilla: ServerFunctionLibrary.reload
func LoadFunctions(packs []string, skip func(file string, err error)) (Functions, error) {
	out := Functions{}
	filters := LoadFilters(packs)
	for i, pack := range packs {
		dropHidden(out, filters[i], "function/", functionExt)
		if err := withPack(pack, func(fsys fs.FS) error { return loadPackFunctions(fsys, out, skip) }); err != nil {
			return nil, fmt.Errorf("data pack %s: %w", filepath.Base(pack), err)
		}
	}
	return out, nil
}

// withPack runs f on the contents of a pack folder or .zip file.
func withPack(pack string, f func(fs.FS) error) error {
	fsys, err := packFS(pack)
	if err != nil {
		return err
	}
	return f(fsys)
}

// packFS opens a pack folder, or a .zip file read whole so no handle stays open,
// with the pack.mcmeta overlays that apply to this format layered over it.
// vanilla: FilePackResources, Pack.readPackMetadata
func packFS(pack string) (fs.FS, error) {
	base, err := openBase(pack)
	if err != nil {
		return nil, err
	}
	return withOverlays(base)
}

func openBase(pack string) (fs.FS, error) {
	info, err := os.Stat(pack)
	if err != nil || info.IsDir() {
		return os.DirFS(pack), nil
	}
	data, err := os.ReadFile(pack)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return zr, nil
}

// folderPacks lists worldDir/datapacks' folder and .zip packs by name; one
// without pack.mcmeta is reported through skip.
// vanilla: FolderRepositorySource
func folderPacks(worldDir string, skip func(file string, err error)) ([]string, error) {
	root := filepath.Join(worldDir, Dir)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || (e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".zip")) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var packs []string
	for _, name := range names {
		pack := filepath.Join(root, name)
		if fsys, err := packFS(pack); err != nil {
			skip(pack, err)
			continue
		} else if _, err := fs.Stat(fsys, "pack.mcmeta"); err != nil {
			skip(pack, fmt.Errorf("missing pack.mcmeta"))
			continue
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// loadPackFunctions adds the function files of one pack to out.
func loadPackFunctions(pack fs.FS, out Functions, skip func(file string, err error)) error {
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
		base := path.Join("data", ns.Name(), "function")
		err := fs.WalkDir(pack, base, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && file == base {
					return fs.SkipDir
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(file, functionExt) {
				return nil
			}
			fnPath := strings.TrimSuffix(strings.TrimPrefix(file, base+"/"), functionExt)
			if !validNamespace(ns.Name()) || !validPath(fnPath) {
				skip(file, fmt.Errorf("invalid function id %s:%s", ns.Name(), fnPath))
				return nil
			}
			data, err := fs.ReadFile(pack, file)
			if err != nil {
				return err
			}
			out[ns.Name()+":"+fnPath] = splitLines(string(data))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// splitLines splits at \n, \r\n or a lone \r, with no empty last line after
// a final break.
// vanilla: BufferedReader.lines
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// validNamespace reports whether s is a valid id namespace.
// vanilla: Identifier.isValidNamespace
func validNamespace(s string) bool {
	return validChars(s, false)
}

// validPath reports whether s is a valid id path.
// vanilla: Identifier.isValidPath
func validPath(s string) bool {
	return validChars(s, true)
}

func validChars(s string, slash bool) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || slash && c == '/' {
			continue
		}
		return false
	}
	return true
}
