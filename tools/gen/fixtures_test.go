package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// workspace is a repository root and a reference directory to generate with.
type workspace struct {
	t         *testing.T
	root, ref string
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := &workspace{t: t, root: t.TempDir(), ref: t.TempDir()}
	for _, dir := range []string{"registry", "internal/protocol", "internal/server/entity"} {
		w.mkdir(w.root, dir)
	}
	return w
}

func (w *workspace) mkdir(base, rel string) string {
	w.t.Helper()
	p := filepath.Join(base, rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *workspace) write(base, rel, content string) {
	w.t.Helper()
	w.mkdir(base, filepath.Dir(rel))
	if err := os.WriteFile(filepath.Join(base, rel), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) report(name, content string) { w.write(w.ref, "generated/reports/"+name, content) }

func (w *workspace) zip(rel string, files map[string]string) {
	w.t.Helper()
	w.mkdir(w.ref, filepath.Dir(rel))
	f, err := os.Create(filepath.Join(w.ref, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			w.t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			w.t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) serverJar(files map[string]string) {
	w.zip("versions/"+vanilla.DefaultVersion+"/server-"+vanilla.DefaultVersion+".jar", files)
}

func (w *workspace) gen(args ...string) {
	w.t.Helper()
	if code, _, stderr := runGen(w.root, w.ref, args...); code != 0 {
		w.t.Fatalf("gen %v: exit %d: %s", args, code, stderr)
	}
}

func (w *workspace) genFails(args ...string) string {
	w.t.Helper()
	code, _, stderr := runGen(w.root, w.ref, args...)
	if code == 0 {
		w.t.Fatalf("gen %v succeeded", args)
	}
	return stderr
}

func (w *workspace) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.root, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// squash collapses runs of blanks, so a check does not depend on how gofmt
// lined up a table.
func squash(s string) string { return blanks.ReplaceAllString(s, " ") }

var blanks = regexp.MustCompile(`[ \t]+`)

func (w *workspace) wantContains(rel string, wants ...string) {
	w.t.Helper()
	got := squash(w.read(rel))
	for _, want := range wants {
		if !strings.Contains(got, squash(want)) {
			w.t.Errorf("%s lacks %q (%d bytes written)", rel, want, len(got))
		}
	}
}

func j(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestTagdefsKeepReferencesAndNestedNames(t *testing.T) {
	w := newWorkspace(t)
	w.serverJar(map[string]string{
		"data/minecraft/tags/block/logs.json":              `{"values": ["minecraft:oak_log", "#minecraft:oak_logs"]}`,
		"data/minecraft/tags/block/mineable/pickaxe.json":  `{"values": ["minecraft:stone"]}`,
		"data/minecraft/tags/worldgen/biome/is_hot.json":   `{"values": []}`,
		"data/minecraft/tags/unlisted_registry/other.json": `{"values": ["minecraft:x"]}`,
	})
	w.gen("tagdefs")
	got := w.read("registry/tagdefs_gen.go")
	for _, want := range []string{
		`"minecraft:block": {`,
		`"minecraft:logs":`, `{"minecraft:oak_log", "#minecraft:oak_logs"}`,
		`"minecraft:mineable/pickaxe":`,
		`"minecraft:worldgen/biome": {`, `"minecraft:is_hot":`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "unlisted_registry") || strings.Contains(got, "minecraft:x") {
		t.Errorf("a registry outside the list was read:\n%s", got)
	}
}

// biomeRegistry writes the registry file the biomespawns generator reads biome names from.
func (w *workspace) biomeRegistry(names ...string) {
	var entries strings.Builder
	for _, n := range names {
		entries.WriteString("\t\t\"minecraft:" + n + "\",\n")
	}
	w.write(w.root, "registry/registries_gen.go", "package registry\nvar Synchronized = []Registry{\n\t{Name: \"minecraft:worldgen/biome\", Entries: []string{\n"+entries.String()+"\t}},\n}")
}

func TestLanguageRewritesFormatsThenAppliesDeprecations(t *testing.T) {
	w := newWorkspace(t)
	w.zip("client/client.jar", map[string]string{
		"assets/minecraft/lang/en_us.json": j(map[string]string{
			"a.count":   "%d items, %.1f% and %2$d",
			"b.old":     "kept",
			"c.removed": "gone",
			"d.new":     "Caf\u00e9",
			"e.orphan":  "dropped with its source",
		}),
		"assets/minecraft/lang/deprecated.json": j(map[string]any{
			"removed": []string{"c.removed"},
			"renamed": map[string]string{"d.new": "d.old", "missing": "e.orphan"},
		}),
	})
	w.gen("language")
	got := w.read("internal/protocol/language_gen.go")
	for _, want := range []string{`"a.count"`, `"%s items, %s% and %2$s"`, `"b.old"`, `"d.old"`, `"Café"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"c.removed", "d.new", "e.orphan"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q survived the deprecations:\n%s", unwanted, got)
		}
	}
}

func TestRegistriesResolveTagsAndRefuseAStaleReport(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:block": {"entries": {
		"minecraft:stone": {"protocol_id": 0}, "minecraft:dirt": {"protocol_id": 1}}}}`)
	dump := j(map[string]any{
		"version": vanilla.DefaultVersion,
		"registries": []any{
			map[string]any{"name": "minecraft:biome", "entries": []string{"minecraft:plains", "minecraft:desert"}},
			map[string]any{"name": "minecraft:banner_pattern", "entries": []string{"minecraft:cross"}},
		},
		// Neither the registries nor the built-in ones arrive sorted.
		"tags": []any{
			map[string]any{"registry": "minecraft:biome", "tags": []any{
				map[string]any{"name": "minecraft:b", "ids": []int{1}},
				map[string]any{"name": "minecraft:a", "ids": []int{0, 1}},
			}},
			map[string]any{"registry": "minecraft:banner_pattern", "tags": []any{
				map[string]any{"name": "minecraft:no_item_required", "ids": []int{0}},
			}},
			map[string]any{"registry": "minecraft:fluid", "tags": []any{
				map[string]any{"name": "minecraft:water", "ids": []int{1}},
			}},
			map[string]any{"registry": "minecraft:block", "tags": []any{
				map[string]any{"name": "minecraft:base", "ids": []int{1, 0}},
			}},
		},
	})
	w.write(w.root, "dump.json", dump)
	runWith := func(args ...string) (int, string) {
		code, _, stderr := runGen(w.root, w.ref, append([]string{"-dump", filepath.Join(w.root, "dump.json")}, args...)...)
		return code, stderr
	}
	if code, stderr := runWith("registries"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := w.read("registry/registries_gen.go")
	for _, want := range []string{
		`const Version = "` + vanilla.DefaultVersion + `"`,
		`{Name: "minecraft:a", Entries: []string{`, // tags sorted by name
		`{Name: "minecraft:base", IDs: []int32{1, 0}}`,
		"var BlockTags = map[string][]string{\n\t\"minecraft:base\": {\n\t\t\"minecraft:dirt\",\n\t\t\"minecraft:stone\",", // members sorted
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if a, b := strings.Index(got, `Name: "minecraft:a"`), strings.Index(got, `Name: "minecraft:b"`); a < 0 || b < a {
		t.Error("tags are not sorted by name")
	}
	for _, order := range [][2]string{
		{`{Registry: "minecraft:banner_pattern"`, `{Registry: "minecraft:biome"`},
		{`{Registry: "minecraft:block"`, `{Registry: "minecraft:fluid"`},
	} {
		if a, b := strings.Index(got, order[0]), strings.Index(got, order[1]); a < 0 || b < a {
			t.Errorf("%s should come before %s:\n%s", order[0], order[1], got)
		}
	}
	// IDs can remain in range across releases while naming different blocks.
	w.write(w.root, "dump.json", strings.Replace(dump, `"`+vanilla.DefaultVersion+`"`, `"1.0"`, 1))
	if code, stderr := runWith("registries"); code != 1 || !strings.Contains(stderr, "captured from 1.0, expected "+vanilla.DefaultVersion) {
		t.Errorf("exit %d, %q; wanted a version mismatch", code, stderr)
	}
	w.write(w.root, "dump.json", dump)

	// A tag naming a block the report lacks is not written short.
	w.report("registries.json", `{"minecraft:block": {"entries": {"minecraft:stone": {"protocol_id": 0}}}}`)
	if code, stderr := runWith("registries"); code != 1 || !strings.Contains(stderr, "minecraft:base") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}
