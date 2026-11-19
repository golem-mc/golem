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
		name: "ruinedportal",
		doc:  "internal/server/world/ruinedportal.zip: ruined portal structures and the templates they pick from",
		run:  runRuinedPortal,
	})
}

// ruinedPortalArchive is where the packed data pack files are written.
const ruinedPortalArchive = "internal/server/world/ruinedportal.zip"

// ruinedPortalStructures are the structure definitions the server generates,
// under minecraft/worldgen/structure.
var ruinedPortalStructures = []string{"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_mountain", "ruined_portal_nether", "ruined_portal_ocean", "ruined_portal_swamp"}

// ruinedPortalTemplates are the templates RuinedPortalStructure picks from, in
// the order of its two lists.
// vanilla: RuinedPortalStructure.STRUCTURE_LOCATION_PORTALS, STRUCTURE_LOCATION_GIANT_PORTALS
var ruinedPortalTemplates = func() []string {
	var out []string
	for i := 1; i <= 10; i++ {
		out = append(out, fmt.Sprintf("minecraft/structure/ruined_portal/portal_%d.nbt", i))
	}
	for i := 1; i <= 3; i++ {
		out = append(out, fmt.Sprintf("minecraft/structure/ruined_portal/giant_portal_%d.nbt", i))
	}
	return out
}()

// runRuinedPortal packs the structure definitions and the templates, under the
// data pack's own paths and formats, so the reader parses what vanilla does.
func runRuinedPortal(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	names := slices.Clone(ruinedPortalTemplates)
	for _, s := range ruinedPortalStructures {
		names = append(names, "minecraft/worldgen/structure/"+s+".json")
	}
	slices.Sort(names)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range names {
		b, err := fs.ReadFile(dp.FS, name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".json") {
			if err := checkRuinedPortalDefinition(b); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		// A template is gzip already; Store keeps the archive from deflating it
		// again. The zero Modified time keeps the archive byte-stable.
		method := zip.Deflate
		if name[len(name)-4:] == ".nbt" {
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
	return e.emitBytes(ruinedPortalArchive, out.Bytes())
}

func checkRuinedPortalDefinition(data []byte) error {
	var definition struct {
		Type           string         `json:"type"`
		Biomes         string         `json:"biomes"`
		Step           string         `json:"step"`
		SpawnOverrides map[string]any `json:"spawn_overrides"`
		Setups         []struct {
			Placement  string  `json:"placement"`
			AirPocket  float32 `json:"air_pocket_probability"`
			Mossiness  float32 `json:"mossiness"`
			Overgrown  bool    `json:"overgrown"`
			Vines      bool    `json:"vines"`
			Cold       bool    `json:"can_be_cold"`
			Blackstone bool    `json:"replace_with_blackstone"`
			Weight     float32 `json:"weight"`
		} `json:"setups"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return err
	}
	if definition.Type != "minecraft:ruined_portal" || definition.Step != "surface_structures" || len(definition.SpawnOverrides) != 0 || len(definition.Setups) == 0 {
		return fmt.Errorf("unsupported ruined portal definition")
	}
	for _, s := range definition.Setups {
		switch s.Placement {
		case "on_land_surface", "on_ocean_floor", "in_nether", "in_mountain", "underground", "partly_buried":
		default:
			return fmt.Errorf("unsupported placement %q", s.Placement)
		}
		if s.Weight <= 0 || s.Mossiness < 0 || s.Mossiness > 1 || s.AirPocket < 0 || s.AirPocket > 1 {
			return fmt.Errorf("unsupported setup %+v", s)
		}
	}
	return nil
}
