package datapack

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadFunctionsLayersThePacks checks the function files read from a
// world's packs: ids from namespace and path, a later pack's file replacing
// an earlier one's, a folder with no pack.mcmeta and a file named with an
// invalid id skipped.
func TestLoadFunctionsLayersThePacks(t *testing.T) {
	world := t.TempDir()
	if got, err := LoadFunctions(nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("no datapacks folder: %v, %v; want none", got, err)
	}
	packs := filepath.Join(world, Dir)
	write(t, filepath.Join(packs, "a", "pack.mcmeta"), "{}")
	write(t, filepath.Join(packs, "a", "data", "ns", "function", "x.mcfunction"), "say a\r\nsay b\n")
	write(t, filepath.Join(packs, "a", "data", "ns", "function", "sub", "y.mcfunction"), "say y")
	write(t, filepath.Join(packs, "a", "data", "ns", "function", "Bad.mcfunction"), "say bad")
	write(t, filepath.Join(packs, "a", "data", "other", "tags", "function", "t.json"), "{}")
	write(t, filepath.Join(packs, "b", "pack.mcmeta"), "{}")
	write(t, filepath.Join(packs, "b", "data", "ns", "function", "x.mcfunction"), "say override")
	write(t, filepath.Join(packs, "c", "data", "ns", "function", "z.mcfunction"), "say nometa")

	var skipped []string
	skip := func(file string, _ error) { skipped = append(skipped, filepath.Base(file)) }
	dirs, err := folderPacks(world, skip)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadFunctions(dirs, skip)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got["ns:x"], []string{"say override"}) {
		t.Errorf("ns:x = %q, want pack b's", got["ns:x"])
	}
	if !slices.Equal(got["ns:sub/y"], []string{"say y"}) {
		t.Errorf("ns:sub/y = %q, want [say y]", got["ns:sub/y"])
	}
	if len(got) != 2 {
		t.Errorf("loaded %v, want ns:x and ns:sub/y alone", got)
	}
	slices.Sort(skipped)
	if want := []string{"Bad.mcfunction", "c"}; !slices.Equal(skipped, want) {
		t.Errorf("skipped %v, want %v", skipped, want)
	}
	if lines := splitLines("a\r\nb\rc\n"); !slices.Equal(lines, []string{"a", "b", "c"}) {
		t.Errorf("splitLines = %q", lines)
	}
}

// TestLoadFunctionTagsLayersThePacks checks the tag files read from a world's
// packs: entries of a later pack following an earlier pack's, replace
// dropping them, string and object entries, tag references, and a file that
// does not parse skipped.
func TestLoadFunctionTagsLayersThePacks(t *testing.T) {
	world := t.TempDir()
	packs := filepath.Join(world, Dir)
	tag := func(pack, name, body string) {
		write(t, filepath.Join(packs, pack, "data", "ns", "tags", "function", name+".json"), body)
	}
	write(t, filepath.Join(packs, "a", "pack.mcmeta"), "{}")
	write(t, filepath.Join(packs, "b", "pack.mcmeta"), "{}")
	write(t, filepath.Join(packs, "c", "pack.mcmeta"), "{}")
	tag("a", "load", `{"values":["ns:one","#ns:more",{"id":"two","required":false}]}`)
	tag("b", "load", `{"values":["ns:three"]}`)
	tag("a", "gone", `{"values":["ns:one"]}`)
	tag("b", "gone", `{"replace":true,"values":["ns:two"]}`)
	tag("a", "bad", `{"values":[`)

	var skipped []string
	skip := func(file string, _ error) { skipped = append(skipped, filepath.Base(file)) }
	dirs, err := folderPacks(world, skip)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadFunctionTags(dirs, skip)
	if err != nil {
		t.Fatal(err)
	}
	want := []TagEntry{
		{ID: "ns:one", Required: true}, {ID: "ns:more", Tag: true, Required: true},
		{ID: "minecraft:two"}, {ID: "ns:three", Required: true},
	}
	if !slices.Equal(got["ns:load"], want) {
		t.Errorf("ns:load = %+v, want %+v", got["ns:load"], want)
	}
	if want := []TagEntry{{ID: "ns:two", Required: true}}; !slices.Equal(got["ns:gone"], want) {
		t.Errorf("ns:gone = %+v, want %+v", got["ns:gone"], want)
	}
	if _, ok := got["ns:bad"]; ok || !slices.Equal(skipped, []string{"bad.json"}) {
		t.Errorf("bad tag loaded or skipped = %v, want it skipped", skipped)
	}
}

// TestZipPackLoads checks a .zip pack is listed as file/<name>.zip and its
// functions are read.
func TestZipPackLoads(t *testing.T) {
	world := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"pack.mcmeta":                       `{"pack":{"description":"zipped"}}`,
		"data/ns/function/hello.mcfunction": "say hi\n",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(world, Dir, "p.zip"), buf.String())
	write(t, filepath.Join(world, Dir, "bad.zip"), "not a zip")
	repo, err := Open(world, nil, nil, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := repo.Pack("file/p.zip")
	if !ok || string(p.Description) != `"zipped"` {
		t.Fatalf("pack = %+v, %v", p, ok)
	}
	if _, ok := repo.Pack("file/bad.zip"); ok {
		t.Fatal("corrupt zip listed")
	}
	fns, err := LoadFunctions(repo.Dirs(), nil)
	if err != nil || len(fns["ns:hello"]) == 0 {
		t.Fatalf("functions = %v, %v", fns, err)
	}
}

func TestOverlayDirectoriesLayerOverThePack(t *testing.T) {
	world := t.TempDir()
	pack := filepath.Join(world, Dir, "p")
	write(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"o","min_format":121,"max_format":121},"overlays":{"entries":[{"min_format":121,"max_format":121,"directory":"new"},{"min_format":1,"max_format":50,"directory":"old"}]}}`)
	write(t, filepath.Join(pack, "data/ns/function/f.mcfunction"), "return 1")
	write(t, filepath.Join(pack, "new/data/ns/function/f.mcfunction"), "return 2")
	write(t, filepath.Join(pack, "new/data/ns/function/only.mcfunction"), "return 5")
	write(t, filepath.Join(pack, "old/data/ns/function/f.mcfunction"), "return 3")
	write(t, filepath.Join(pack, "data/ns/tags/function/t.json"), `{"values":["ns:f"]}`)
	write(t, filepath.Join(pack, "new/data/ns/tags/function/t.json"), `{"values":["ns:only"]}`)

	write(t, filepath.Join(pack, "data/ns/structure/s.nbt"), "base")
	write(t, filepath.Join(pack, "new/data/ns/structure/s.nbt"), "overlay")

	skip := func(file string, err error) { t.Errorf("skipped %s: %v", file, err) }
	dirs, err := folderPacks(world, skip)
	if err != nil {
		t.Fatal(err)
	}
	if data, ok, err := StructureFile(world, dirs, "ns:s"); err != nil || !ok || string(data) != "overlay" {
		t.Errorf("StructureFile = %q, %v, %v, want overlay", data, ok, err)
	}
	if ids := ListStructures(world, dirs); !slices.Equal(ids, []string{"ns:s"}) {
		t.Errorf("ListStructures = %q, want [ns:s]", ids)
	}
	fns, err := LoadFunctions(dirs, skip)
	if err != nil {
		t.Fatal(err)
	}
	if got := fns["ns:f"]; !slices.Equal(got, []string{"return 2"}) {
		t.Errorf("ns:f = %q, want [return 2]", got)
	}
	if _, ok := fns["ns:only"]; !ok {
		t.Error("ns:only missing")
	}
	tags, err := LoadFunctionTags(dirs, skip)
	if err != nil {
		t.Fatal(err)
	}
	if got := tags["ns:t"]; len(got) != 1 || got[0].ID != "ns:only" {
		t.Errorf("ns:t = %+v, want only ns:only", got)
	}
}
