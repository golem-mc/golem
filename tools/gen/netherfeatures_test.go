package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func TestNetherFeaturesPortalDefinition(t *testing.T) {
	valid := `{"type":"minecraft:ruined_portal","biomes":"#minecraft:has_structure/ruined_portal_nether","step":"surface_structures","spawn_overrides":{},"setups":[{"placement":"in_nether","air_pocket_probability":0,"mossiness":0,"weight":1}]}`
	for _, tc := range []struct {
		name, definition string
		valid            bool
	}{
		{"valid", valid, true},
		{"unknown field", strings.Replace(valid, `"mossiness":0`, `"unknown":0`, 1), false},
		{"two setups", strings.Replace(valid, `}]}`, `},{"placement":"in_nether","weight":1}]}`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			w.mkdir(w.root, "internal/server/world")
			files := map[string]string{"data/minecraft/worldgen/structure/ruined_portal_nether.json": tc.definition}
			for _, name := range []string{"basalt_pillar", "glowstone", "glowstone_extra", "weeping_vines", "twisting_vines", "crimson_forest_vegetation", "warped_forest_vegetation", "nether_sprouts"} {
				files["data/minecraft/worldgen/placed_feature/"+name+".json"] = `{"feature":{"type":"minecraft:simple_block","to_place":"minecraft:air"},"placement":[]}`
			}
			w.zip("versions/26.3/server-26.3.jar", files)
			e := &env{root: w.root, ref: vanilla.Ref{Dir: w.ref, Version: "26.3"}, stdout: io.Discard}
			err := runNetherFeatures(e)
			if (err == nil) != tc.valid {
				t.Fatalf("error=%v, valid=%v", err, tc.valid)
			}
			if tc.valid {
				got, err := os.ReadFile(filepath.Join(w.root, "internal/server/world/nether_ruinedportal.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, []byte(valid)) {
					t.Fatal("portal definition changed")
				}
			}
		})
	}
}
