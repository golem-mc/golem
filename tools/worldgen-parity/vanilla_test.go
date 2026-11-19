package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCacheConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stored    cacheConfig
		requested cacheConfig
		match     bool
	}{
		{"no version", cacheConfig{}, cacheConfig{Version: "26.3"}, false},
		{"26.3", cacheConfig{Version: "26.3"}, cacheConfig{Version: "26.3"}, true},
		{"different version", cacheConfig{Version: "26.3"}, cacheConfig{Version: "26.1"}, false},
		{"pack versus plain", cacheConfig{Version: "26.3", Datapacks: []string{"terrain"}}, cacheConfig{Version: "26.3"}, false},
		{"different pack", cacheConfig{Version: "26.3", Datapacks: []string{"terrain"}}, cacheConfig{Version: "26.3", Datapacks: []string{"carved"}}, false},
		{"same packs", cacheConfig{Version: "26.3", Datapacks: []string{"terrain", "extra"}}, cacheConfig{Version: "26.3", Datapacks: []string{"terrain", "extra"}}, true},
		{"pack order", cacheConfig{Version: "26.3", Datapacks: []string{"terrain", "extra"}}, cacheConfig{Version: "26.3", Datapacks: []string{"extra", "terrain"}}, false},
		{"structures", cacheConfig{Version: "26.3", NoStructures: true}, cacheConfig{Version: "26.3"}, false},
		{"level type", cacheConfig{Version: "26.3", LevelType: levelTypeID("AMPLIFIED")}, cacheConfig{Version: "26.3"}, false},
		{"same level type", cacheConfig{Version: "26.3", LevelType: levelTypeID("amplified")}, cacheConfig{Version: "26.3", LevelType: levelTypeID("minecraft:amplified")}, true},
		{"normal level type", cacheConfig{Version: "26.3"}, cacheConfig{Version: "26.3", LevelType: levelTypeID("minecraft:normal")}, true},
		{"flat", cacheConfig{Version: "26.3", LevelType: levelTypeID("flat")}, cacheConfig{Version: "26.3", LevelType: "minecraft:flat"}, true},
		{"generator settings", cacheConfig{Version: "26.3", LevelType: "minecraft:flat", GeneratorSettings: `{"layers":[]}`}, cacheConfig{Version: "26.3", LevelType: "minecraft:flat"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			entry := filepath.Join(dir, "entry")
			if err := os.Mkdir(entry, 0755); err != nil {
				t.Fatal(err)
			}
			meta := cacheMeta{cacheConfig: tc.stored, Dimension: "the_nether", Seed: 0, Range: chunkRange{-4, -4, 3, 3}, Order: cacheOrder}
			data, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			if tc.stored.Version == "" { // Exercise an actual old meta.json with no version key.
				data = []byte(`{"dimension":"the_nether","seed":0,"range":{"x0":-4,"z0":-4,"x1":3,"z1":3},"order":"server-start"}`)
			}
			if err := os.WriteFile(filepath.Join(entry, "meta.json"), data, 0644); err != nil {
				t.Fatal(err)
			}
			got := findCached(dir, dimensions["the_nether"], 0, chunkRange{-2, -2, 1, 1}, tc.requested, false)
			if (got != "") != tc.match {
				t.Fatalf("cache = %q, want match %v", got, tc.match)
			}
			if got := findCached(dir, dimensions["the_nether"], 0, chunkRange{-5, -5, 1, 1}, tc.requested, false); got != "" {
				t.Fatal("accepted incomplete range")
			}
		})
	}
	seen := map[string]bool{}
	for _, c := range []cacheConfig{{Version: "26.3"}, {Version: "26.3", Datapacks: []string{"terrain"}}, {Version: "26.3", Datapacks: []string{"carved"}}, {Version: "26.3", NoStructures: true}, {Version: "26.3", LevelType: "minecraft:amplified"}, {Version: "26.3", LevelType: "minecraft:large_biomes"}, {Version: "26.3", LevelType: "minecraft:flat"}, {Version: "26.3", LevelType: "minecraft:flat", GeneratorSettings: `{"layers":[]}`}} {
		suffix := c.suffix()
		if seen[suffix] {
			t.Fatalf("cache path collision for %+v", c)
		}
		seen[suffix] = true
	}
}

func TestDatapackCopy(t *testing.T) {
	paths := []string{"datapacks/nether-no-features", "datapacks/nether-no-features-carvers-kept"}
	worldDir := t.TempDir()
	if err := copyDatapacks(worldDir, paths); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		for _, biome := range []string{"nether_wastes", "soul_sand_valley", "crimson_forest", "warped_forest", "basalt_deltas"} {
			rel := filepath.Join("data", "minecraft", "worldgen", "biome", biome+".json")
			want, err := os.ReadFile(filepath.Join(path, rel))
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(worldDir, "datapacks", filepath.Base(path), rel))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, want) {
				t.Fatal("copy changed biome")
			}
			var data struct {
				Features []any
				Carvers  any
			}
			if err := json.Unmarshal(got, &data); err != nil {
				t.Fatal(err)
			}
			if data.Features == nil || len(data.Features) != 0 {
				t.Fatal("features not emptied")
			}
			if i == 0 {
				if carvers, ok := data.Carvers.([]any); !ok || len(carvers) != 0 {
					t.Fatal("carvers not emptied")
				}
			} else if data.Carvers != "minecraft:nether_cave" {
				t.Fatal("carvers not preserved")
			}
		}
	}
	for _, paths := range [][]string{{"missing"}, {paths[0], paths[0]}} {
		if _, err := datapackNames(paths); err == nil {
			t.Fatalf("accepted %v", paths)
		}
	}
}

func TestCacheRequiresLightTimeline(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "entry")
	if err := os.Mkdir(entry, 0755); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{0, 1, cacheLighting} {
		meta := cacheMeta{Version: "26.3", Dimension: "overworld", Seed: 0, Range: chunkRange{-4, -4, 3, 3}, Order: cacheOrderFor(dimensions["overworld"]), Lighting: version}
		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(entry, "meta.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		got := findCached(dir, dimensions["overworld"], 0, chunkRange{-2, -2, 1, 1}, meta.cacheConfig, false)
		if (got != "") != (version == cacheLighting) {
			t.Fatalf("lighting %d: cache=%q", version, got)
		}
	}
}

func TestCacheEntitiesRecordedOnRequest(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "entry")
	if err := os.Mkdir(entry, 0755); err != nil {
		t.Fatal(err)
	}
	meta := cacheMeta{Version: "26.3", Dimension: "the_nether", Range: chunkRange{-4, -4, 3, 3}, Order: cacheOrderFor(dimensions["the_nether"])}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	find := func(recordEntities bool) string {
		return findCached(dir, dimensions["the_nether"], 0, chunkRange{-2, -2, 1, 1}, meta.cacheConfig, recordEntities)
	}
	if find(false) != entry || find(true) != "" {
		t.Fatalf("without entities/: plain %q, -record-entities %q", find(false), find(true))
	}
	if err := os.Mkdir(filepath.Join(entry, "entities"), 0755); err != nil {
		t.Fatal(err)
	}
	if find(true) != entry {
		t.Fatalf("with entities/: -record-entities %q", find(true))
	}
}

func TestPropertyValue(t *testing.T) {
	got := propertyValue(`{"layers":[{"block":"minecraft:stone","height":1}],"biome":"a\b"}`)
	want := `{"layers"\:[{"block"\:"minecraft\:stone","height"\:1}],"biome"\:"a\\b"}`
	if got != want {
		t.Fatalf("propertyValue = %s, want %s", got, want)
	}
}
