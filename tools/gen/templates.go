package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"

	"github.com/golem-mc/golem/nbt"
)

// packTemplates validates and packs a fixed set of gzipped data-pack templates.
func packTemplates(e *env, glob string, want int, out string, check func(name string, tag nbt.Compound) error) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	names, err := fs.Glob(dp.FS, glob)
	if err != nil {
		return err
	}
	if len(names) != want {
		return fmt.Errorf("%s has %d templates, want %d; review template selection", glob, len(names), want)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		data, err := fs.ReadFile(dp.FS, name)
		if err != nil {
			return err
		}
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		raw, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		_, root, err := nbt.Decode(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := check(name, root.(nbt.Compound)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return e.emitBytes(out, buf.Bytes())
}
