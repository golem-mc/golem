package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
)

func init() {
	register(generator{name: "nether-features", doc: "internal/server/world/nether_features.txt and nether_ruinedportal.json: the Nether's features and portal setup", run: runNetherFeatures})
}

func runNetherFeatures(e *env) error {
	if e.ref.Version != "26.3" {
		e.logf("skipped: needs 26.3")
		return nil
	}
	pack, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer pack.Close()
	data, err := fs.Sub(pack, "minecraft/worldgen")
	if err != nil {
		return err
	}
	r := featureReader{fsys: data}
	var out bytes.Buffer
	for _, name := range []string{"basalt_pillar", "glowstone", "glowstone_extra", "weeping_vines", "twisting_vines", "crimson_forest_vegetation", "warped_forest_vegetation", "nether_sprouts"} {
		doc, err := r.placed(name)
		if err != nil {
			return err
		}
		if bad := unmodeled(doc); len(bad) > 0 {
			return fmt.Errorf("%s: unmodeled %v", name, bad)
		}
		line, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "%s\t%s\n", name, line)
	}
	portal, err := fs.ReadFile(data, "structure/ruined_portal_nether.json")
	if err != nil {
		return err
	}
	if err := checkRuinedPortalDefinition(portal); err != nil {
		return err
	}
	var definition struct {
		Setups []json.RawMessage `json:"setups"`
	}
	if err := json.Unmarshal(portal, &definition); err != nil {
		return err
	}
	if len(definition.Setups) != 1 {
		return fmt.Errorf("nether ruined portal: want exactly one setup")
	}
	if err := e.emitBytes("internal/server/world/nether_ruinedportal.json", portal); err != nil {
		return err
	}
	return e.emitBytes("internal/server/world/nether_features.txt", out.Bytes())
}
