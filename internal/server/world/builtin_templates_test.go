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

func TestTemplateStoreBuiltinFallback(t *testing.T) {
	const id = "minecraft:igloo/top"
	files := templateStoreFiles{}
	store := NewTemplateStore(files, 4440)
	builtin, ok := store.Get(id)
	if !ok || builtin.Size != [3]int32{7, 5, 8} {
		t.Fatalf("built-in template = %v, found %v", builtin, ok)
	}
	cached, _ := store.Get(id)
	if cached != builtin {
		t.Fatal("built-in template was not cached")
	}
	for _, missing := range []string{"test:igloo/top", "minecraft:does_not_exist", "minecraft:igloo"} {
		if _, ok := store.Get(missing); ok {
			t.Errorf("unexpected template %s", missing)
		}
	}

	override := &Template{Size: [3]int32{1, 2, 3}}
	data, err := override.Encode(4440)
	if err != nil {
		t.Fatal(err)
	}
	files[id] = data
	store.Clear()
	got, ok := store.Get(id)
	if !ok || got.Size != override.Size {
		t.Fatalf("file did not override built-in template: %v, found %v", got, ok)
	}
	delete(files, id)
	store.Remove(id)
	got, ok = store.Get(id)
	if !ok || got.Size != builtin.Size {
		t.Fatalf("removing override did not restore built-in: %v, found %v", got, ok)
	}
}
