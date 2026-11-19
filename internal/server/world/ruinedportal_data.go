package world

//go:generate go run ../../../tools/gen ruinedportal

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// ruinedPortalArchive holds the ruined portal structure definitions and their
// templates under their data pack paths (tools/gen ruinedportal).
//
//go:embed ruinedportal.zip
var ruinedPortalArchive []byte

//go:embed nether_ruinedportal.json
var netherRuinedPortalDefinition []byte

// ruinedPortalData holds the setups and templates of ruined portals.
// vanilla: RuinedPortalStructure
type ruinedPortalData struct {
	setups map[string][]ruinedPortalSetup
	// portals and giants are the templates in the order the start's random
	// index picks them.
	portals, giants []*ruinedPortalTemplate
}

// ruinedPortalTemplate is a template with its id, as pieces save it.
type ruinedPortalTemplate struct {
	*Template
	id string
}

var (
	ruinedPortalOnce sync.Once
	ruinedPortalDef  *ruinedPortalData
)

// ruinedPortals returns the archive's definition; it panics when the embedded
// data cannot be read, which no input can cause.
func ruinedPortals() *ruinedPortalData {
	ruinedPortalOnce.Do(func() {
		d, err := loadRuinedPortal(ruinedPortalArchive)
		if err != nil {
			panic("ruined portal data: " + err.Error())
		}
		setups, err := decodeRuinedPortalDefinition(netherRuinedPortalDefinition)
		if err != nil || len(setups) != 1 {
			panic(fmt.Sprintf("Nether ruined portal data: %v (%d setups)", err, len(setups)))
		}
		d.setups[ruinedPortalID] = setups
		ruinedPortalDef = d
	})
	return ruinedPortalDef
}

// loadRuinedPortal reads the definition. A setting the generator does not model
// is an error rather than ignored, so a different structure cannot pass for
// the one it matches.
func loadRuinedPortal(archive []byte) (*ruinedPortalData, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	read := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	d := &ruinedPortalData{setups: map[string][]ruinedPortalSetup{}}
	for _, id := range []string{"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_mountain", "ruined_portal_nether", "ruined_portal_ocean", "ruined_portal_swamp"} {
		b, err := read("minecraft/worldgen/structure/" + id + ".json")
		if err != nil {
			return nil, err
		}
		setups, err := decodeRuinedPortalDefinition(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		d.setups["minecraft:"+id] = setups
	}
	load := func(name string) (*ruinedPortalTemplate, error) {
		b, err := read("minecraft/structure/ruined_portal/" + name + ".nbt")
		if err != nil {
			return nil, err
		}
		t, err := ParseTemplate(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(t.Palettes) != 1 {
			return nil, fmt.Errorf("%s: %d palettes", name, len(t.Palettes))
		}
		return &ruinedPortalTemplate{Template: t, id: "minecraft:ruined_portal/" + name}, nil
	}
	for i := 1; i <= 10; i++ {
		t, err := load(fmt.Sprintf("portal_%d", i))
		if err != nil {
			return nil, err
		}
		d.portals = append(d.portals, t)
	}
	for i := 1; i <= 3; i++ {
		t, err := load(fmt.Sprintf("giant_portal_%d", i))
		if err != nil {
			return nil, err
		}
		d.giants = append(d.giants, t)
	}
	return d, nil
}

// ruinedPortalSetup is one weighted setup from RuinedPortalStructure's codec.
type ruinedPortalSetup struct {
	AirPocket  float32 `json:"air_pocket_probability"`
	Cold       bool    `json:"can_be_cold"`
	Mossiness  float32 `json:"mossiness"`
	Overgrown  bool    `json:"overgrown"`
	Placement  string  `json:"placement"`
	Blackstone bool    `json:"replace_with_blackstone"`
	Vines      bool    `json:"vines"`
	Weight     float32 `json:"weight"`
}

func decodeRuinedPortalDefinition(b []byte) ([]ruinedPortalSetup, error) {
	var j struct {
		Type          string              `json:"type"`
		Biomes        string              `json:"biomes"`
		Step          string              `json:"step"`
		SpawnOverride map[string]any      `json:"spawn_overrides"`
		Setups        []ruinedPortalSetup `json:"setups"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, err
	}
	if len(j.Setups) == 0 || j.Type != "minecraft:ruined_portal" || j.Step != "surface_structures" || len(j.SpawnOverride) != 0 {
		return nil, fmt.Errorf("unsupported ruined portal settings")
	}
	for _, s := range j.Setups {
		switch s.Placement {
		case "in_nether", "on_land_surface", "on_ocean_floor", "partly_buried", "underground", "in_mountain":
		default:
			return nil, fmt.Errorf("unsupported ruined portal placement: %s", s.Placement)
		}
		if s.AirPocket < 0 || s.AirPocket > 1 || s.Mossiness < 0 || s.Mossiness > 1 || s.Weight <= 0 {
			return nil, fmt.Errorf("unsupported ruined portal setup: %+v", s)
		}
	}
	return j.Setups, nil
}
