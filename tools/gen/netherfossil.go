package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"
)

func init() {
	register(generator{
		name: "netherfossil",
		doc:  "internal/server/world/netherfossil.zip: the nether fossil's structure, its structure set and its fourteen templates",
		run:  runNetherFossil,
	})
}

// netherFossilArchive is where the packed data pack files are written.
const netherFossilArchive = "internal/server/world/netherfossil.zip"

// netherFossilFiles are the data pack files the fossil reads: the structure,
// its structure set and the templates NetherFossilPieces picks from.
// vanilla: NetherFossilPieces.FOSSILS
var netherFossilFiles = func() []string {
	out := []string{
		"minecraft/worldgen/structure/nether_fossil.json",
		"minecraft/worldgen/structure_set/nether_fossils.json",
	}
	for i := 1; i <= 14; i++ {
		out = append(out, fmt.Sprintf("minecraft/structure/nether_fossils/fossil_%d.nbt", i))
	}
	return out
}()

// strictJSON decodes b into v and stops on a field v does not name, so a data
// pack field the port does not model fails the generator.
func strictJSON(name string, b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w (a field the nether fossil port does not model?)", name, err)
	}
	return nil
}

// runNetherFossil packs the files under the data pack's own paths and formats,
// after checking that the structure and its set hold only what the port models.
func runNetherFossil(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	names := slices.Clone(netherFossilFiles)
	slices.Sort(names)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range names {
		b, err := fs.ReadFile(dp.FS, name)
		if err != nil {
			return err
		}
		switch {
		case strings.Contains(name, "/structure/nether_fossil."):
			var s struct {
				Type   string `json:"type"`
				Biomes string `json:"biomes"`
				Height struct {
					Type string `json:"type"`
					Max  struct {
						BelowTop int `json:"below_top"`
					} `json:"max_inclusive"`
					Min struct {
						Absolute int `json:"absolute"`
					} `json:"min_inclusive"`
				} `json:"height"`
				SpawnOverrides map[string]any `json:"spawn_overrides"`
				Step           string         `json:"step"`
				Adaptation     string         `json:"terrain_adaptation"`
			}
			if err := strictJSON(name, b, &s); err != nil {
				return err
			}
			if s.Type != "minecraft:nether_fossil" || s.Height.Type != "minecraft:uniform" || s.Step != "underground_decoration" ||
				s.Adaptation != "beard_thin" || len(s.SpawnOverrides) != 0 {
				return fmt.Errorf("%s: %+v differs from what the nether fossil port models", name, s)
			}
		case strings.Contains(name, "/structure_set/"):
			var s struct {
				Placement struct {
					Type       string `json:"type"`
					Salt       int    `json:"salt"`
					Separation int    `json:"separation"`
					Spacing    int    `json:"spacing"`
				} `json:"placement"`
				Structures []struct {
					Structure string `json:"structure"`
					Weight    int    `json:"weight"`
				} `json:"structures"`
			}
			if err := strictJSON(name, b, &s); err != nil {
				return err
			}
			if s.Placement.Type != "minecraft:random_spread" || len(s.Structures) != 1 || s.Structures[0].Structure != "minecraft:nether_fossil" {
				return fmt.Errorf("%s: %+v differs from what the nether fossil port models", name, s)
			}
		}
		// A template is gzip already; Store keeps the archive from deflating it
		// again. The zero Modified time keeps the archive byte-stable.
		method := zip.Deflate
		if strings.HasSuffix(name, ".nbt") {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Time{}})
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return e.emitBytes(netherFossilArchive, out.Bytes())
}
