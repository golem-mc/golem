package datapack

import (
	"os"
	"path/filepath"
	"testing"
)

func writePackFile(t *testing.T, pack, rel, body string) {
	t.Helper()
	file := filepath.Join(pack, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFilterHidesLowerPacksFiles checks that a pack's filter removes the
// functions, resources and structures of the packs below it and keeps its
// own, and that a filter that does not parse hides nothing.
// vanilla: MultiPackResourceManager, FallbackResourceManager.getResource
func TestFilterHidesLowerPacksFiles(t *testing.T) {
	root := t.TempDir()
	low, high, broken := filepath.Join(root, "low"), filepath.Join(root, "high"), filepath.Join(root, "broken")
	for _, pack := range []string{low, high} {
		writePackFile(t, pack, "data/x/function/f.mcfunction", "say "+filepath.Base(pack)+"\n")
		writePackFile(t, pack, "data/x/function/keep.mcfunction", "say keep\n")
		writePackFile(t, pack, "data/x/structure/s.nbt", filepath.Base(pack))
	}
	writePackFile(t, low, "data/x/function/low_only.mcfunction", "say low\n")
	writePackFile(t, low, "data/x/recipe/r.json", "{}")
	writePackFile(t, high, "pack.mcmeta", `{"pack":{"description":""},"filter":{"block":[{"namespace":"x","path":"(function/(f|low_only)|recipe/.*|structure/s)\\.(mcfunction|json|nbt)"}]}}`)
	// An invalid pattern fails the whole section.
	writePackFile(t, broken, "pack.mcmeta", `{"pack":{"description":""},"filter":{"block":[{"path":"("}]}}`)
	writePackFile(t, broken, "data/x/function/g.mcfunction", "say g\n")

	fns, err := LoadFunctions([]string{low, high, broken}, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fns["x:low_only"]; ok {
		t.Error("a function of the lower pack survives the filter")
	}
	if got := fns["x:f"]; len(got) != 1 || got[0] != "say high" {
		t.Errorf("x:f = %q, want the filtering pack's own", got)
	}
	if _, ok := fns["x:keep"]; !ok {
		t.Error("a function the filter does not match was hidden")
	}
	if _, ok := fns["x:g"]; !ok {
		t.Error("a pack whose filter does not parse hid something")
	}
	res, err := LoadResources([]string{low, high}, "recipe", func(string, error) {})
	if err != nil || len(res) != 0 {
		t.Errorf("recipes = %v, %v, want none", res, err)
	}
	data, ok, err := StructureFile(root, []string{low, high}, "x:s")
	if err != nil || !ok || string(data) != "high" {
		t.Errorf("structure = %q, %v, %v, want the filtering pack's", data, ok, err)
	}
	if !LoadFilters([]string{low, high}).Builtin("x", "recipe/r.json") || LoadFilters([]string{low, high}).Builtin("y", "recipe/r.json") {
		t.Error("Builtin ignores the namespace pattern")
	}
}
