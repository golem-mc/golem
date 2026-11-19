package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFeaturesOrder(t *testing.T) {
	data := []byte(`{"recording":{"events":[
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00.000000003Z","chunkPosX":3,"chunkPosZ":-3,"status":"minecraft:features","level":"minecraft:overworld"}},
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00Z","chunkPosX":9,"chunkPosZ":9,"status":"minecraft:noise","level":"minecraft:overworld"}},
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00Z","chunkPosX":8,"chunkPosZ":8,"status":"minecraft:features","level":"minecraft:the_nether"}},
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00.000000001Z","chunkPosX":1,"chunkPosZ":-1,"status":"minecraft:features","level":"minecraft:overworld"}},
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00.000000001Z","chunkPosX":2,"chunkPosZ":-2,"status":"minecraft:features","level":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-07T12:00:00.000000002Z","chunkPosX":1,"chunkPosZ":-1,"type":"chunk","dimension":"minecraft:overworld"}},
 {"type":"minecraft.ChunkGeneration","values":{"startTime":"2026-11-07T12:00:00Z","chunkPosX":1,"chunkPosZ":-1,"status":"minecraft:carvers","level":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-06T12:00:00Z","chunkPosX":1,"chunkPosZ":-1,"type":"chunk","dimension":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-07T12:00:00.000000002Z","chunkPosX":2,"chunkPosZ":-2,"type":"chunk","dimension":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-07T12:00:00.000000002Z","chunkPosX":1,"chunkPosZ":-1,"type":"poi","dimension":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-07T12:00:00.000000002Z","chunkPosX":1,"chunkPosZ":-1,"type":"entities","dimension":"minecraft:overworld"}},
 {"type":"minecraft.ChunkRegionRead","values":{"startTime":"2026-11-07T12:00:00.000000002Z","chunkPosX":1,"chunkPosZ":-1,"type":"chunk","dimension":"minecraft:the_nether"}}
 ]}}`)
	for _, status := range []string{"minecraft:carvers", "minecraft:terrain"} {
		t.Run(status, func(t *testing.T) {
			recording := strings.ReplaceAll(string(data), "minecraft:carvers", status)
			got, err := featuresOrder([]byte(recording), "minecraft:overworld")
			if err != nil {
				t.Fatal(err)
			}
			want := []recordedStep{{Pos: [2]int32{1, -1}}, {Pos: [2]int32{2, -2}}, {Pos: [2]int32{1, -1}, Load: true}, {Pos: [2]int32{3, -3}}}
			if !slices.Equal(got, want) {
				t.Fatalf("order = %v, want %v", got, want)
			}
		})
	}
	for name, data := range map[string]string{
		"invalid JSON":      `{`,
		"invalid timestamp": `{"recording":{"events":[{"type":"minecraft.ChunkGeneration","values":{"startTime":"bad","status":"minecraft:features","level":"minecraft:overworld"}}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := featuresOrder([]byte(data), "minecraft:overworld"); err == nil {
				t.Fatal("accepted invalid recording")
			}
		})
	}
}

func TestLoadFeaturesOrder(t *testing.T) {
	dir := t.TempDir()
	if got, err := loadFeaturesOrder(dir); err != nil || got != nil {
		t.Fatalf("missing file = %v, %v", got, err)
	}
	for name, tc := range map[string]struct {
		data string
		want []recordedStep
		bad  bool
	}{
		"empty":        {want: []recordedStep{}},
		"coordinates":  {data: "-21,-25\n-21,-26\n", want: []recordedStep{{Pos: [2]int32{-21, -25}}, {Pos: [2]int32{-21, -26}}}},
		"loads":        {data: "-21,-25\nload -21,-25\n-21,-26\n", want: []recordedStep{{Pos: [2]int32{-21, -25}}, {Pos: [2]int32{-21, -25}, Load: true}, {Pos: [2]int32{-21, -26}}}},
		"light stages": {data: "light_sources -21,-25\ninitialize_light -21,-25\nlight -21,-25\n", want: []recordedStep{{Pos: [2]int32{-21, -25}, Light: "light_sources"}, {Pos: [2]int32{-21, -25}, Light: "initialize_light"}, {Pos: [2]int32{-21, -25}, Light: "light"}}},
		"bad load":     {data: "load bad\n", bad: true},
		"invalid":      {data: "bad\n", bad: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "features.txt"), []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := loadFeaturesOrder(dir)
			if (err != nil) != tc.bad || !slices.Equal(got, tc.want) || (!tc.bad && got == nil) {
				t.Fatalf("loaded %v, %v; want %v, bad=%v", got, err, tc.want, tc.bad)
			}
		})
	}
}
