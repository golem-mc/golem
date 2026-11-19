package vanilla

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ServerJar is the server jar the bundler unpacks on the first
// `reference:run`: the one holding the data pack and the server's classes.
func (r Ref) ServerJar() string {
	return filepath.Join(r.Dir, "versions", r.Version, "server-"+r.Version+".jar")
}

// Libraries is the directory of jars the server needs on its classpath,
// unpacked by the same first run.
func (r Ref) Libraries() string {
	return filepath.Join(r.Dir, "libraries")
}

// DataPack is the vanilla data pack. The file system is rooted at the jar's
// data/ directory, so a recipe is minecraft/recipe/<name>.json.
type DataPack struct {
	fs.FS
	closer io.Closer
}

// Close releases the jar.
func (d *DataPack) Close() error {
	if d.closer == nil {
		return nil
	}
	return d.closer.Close()
}

// DataPack opens the data pack out of the server jar, or out of a data-extract
// directory beside it when there is no jar.
func (r Ref) DataPack() (*DataPack, error) {
	zr, err := zip.OpenReader(r.ServerJar())
	if err == nil {
		sub, err := fs.Sub(zr, "data")
		if err != nil {
			zr.Close()
			return nil, err
		}
		return &DataPack{FS: sub, closer: zr}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	extracted := filepath.Join(r.Dir, "data-extract", "data")
	if _, statErr := os.Stat(extracted); statErr == nil {
		return &DataPack{FS: os.DirFS(extracted)}, nil
	}
	return nil, fmt.Errorf("%s is missing; run `mise run reference:setup` and `mise run reference:run` once so the server unpacks it", r.ServerJar())
}

// Files lists the files under dir whose names end in suffix, as paths
// relative to dir in lexical order. A directory that is not there lists
// nothing.
func Files(fsys fs.FS, dir, suffix string) ([]string, error) {
	var out []string
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, suffix) {
			out = append(out, strings.TrimPrefix(p, dir+"/"))
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	// WalkDir orders a directory's entries by name, which puts "a/b.json"
	// before "a.json"; sorting the paths puts "a.json" first.
	slices.Sort(out)
	return out, err
}

// ReadJSON decodes one JSON file of fsys into v.
func ReadJSON(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Stem is a file name without its directory and extension.
func Stem(name string) string {
	base := path.Base(name)
	return strings.TrimSuffix(base, path.Ext(base))
}
