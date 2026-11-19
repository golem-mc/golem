package world

import "testing"

func TestBuiltinTemplateArchives(t *testing.T) {
	for _, id := range []string{
		"minecraft:end_city/base_floor",
		"minecraft:fossil/skull_1",
		"minecraft:igloo/top",
		"minecraft:ancient_city/city/entrance/entrance_connector",
		"minecraft:woodland_mansion/1x1_a1",
		"minecraft:nether_fossils/fossil_1",
		"minecraft:underwater_ruin/big_brick_1",
		"minecraft:ruined_portal/giant_portal_1",
		"minecraft:shipwreck/rightsideup_backhalf",
	} {
		t.Run(id, func(t *testing.T) {
			tpl, ok := builtinTemplate(id)
			if !ok || len(tpl.Palettes) == 0 || tpl.Size[0] <= 0 {
				t.Fatalf("embedded template %s did not load: %v", id, tpl)
			}
		})
	}
}

type templateStoreFiles map[string][]byte

func (f templateStoreFiles) ReadTemplate(id string) ([]byte, bool, error) {
	data, ok := f[id]
	return data, ok, nil
}

func (f templateStoreFiles) ListTemplates() []string { return nil }
func (f templateStoreFiles) WriteTemplate(id string, data []byte) error {
	f[id] = data
	return nil
}
