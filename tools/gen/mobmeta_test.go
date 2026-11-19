package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mobMetaInput(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/server/mobmeta.go"))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestMobMetaGenerationIsStable(t *testing.T) {
	src := mobMetaInput(t)
	groups, err := parseMobMeta(src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := generateMobMeta(groups)
	if err != nil {
		t.Fatal(err)
	}
	again, err := generateMobMeta(groups)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("nondeterministic output")
	}
	committed, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/server/mobmeta_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, committed) {
		t.Fatal("run mise run gen:mobmeta")
	}
	entity, err := generateMobMetaSources(groups)
	if err != nil {
		t.Fatal(err)
	}
	entityAgain, err := generateMobMetaSources(groups)
	if err != nil {
		t.Fatal(err)
	}
	committedEntity, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/server/entity/mobmeta_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(entity, entityAgain) || !bytes.Equal(entity, committedEntity) {
		t.Fatal("unstable or stale entity source generation")
	}

}

func TestMobMetaMalformedSchemaDoesNotWrite(t *testing.T) {
	src := string(mobMetaInput(t))
	for _, test := range []struct{ name, from, to, want string }{
		{"duplicate field", `mobMeta("air",`, `mobMeta("health",`, "duplicate field"},
		{"missing capture", `s.health = float32(m.Health)`, ``, "missing capture"},
		{"missing member", `mobMeta("health",`, `mobMeta("",`, "missing source coverage"},
		{"unknown member", `s.health = float32(m.Health)`, `s.unknown = 1; s.health = 1`, "unresolved s.unknown"},
		{"uncaptured member", `s.health = float32(m.Health)`, `s.health = s.air`, "unresolved s.air"},
		{"unknown encoder member", `now.health, was.health, true`, `now.unknown, was.health, true`, "unresolved now.unknown"},
		{"previous member", `now.health, was.health, true`, `now.health, was.air, true`, "unresolved was.air"},
		{"duplicate order", `mobMetaEntry{140, 140,`, `mobMetaEntry{200, 140,`, "duplicate metadata ordering"},
		{"bad position", `mobMetaEntry{140, 140,`, `mobMetaEntry{-2, 140,`, "invalid metadata ordering"},
		{"unresolved entry", `mobMetaEntry{140, 140,`, `unknownEntry{140, 140,`, "unresolved encoder"},
		{"unsupported descriptor", `mobMeta("health",`, `unknownMeta("health",`, "unsupported descriptor"},
		{"duplicate source", `{"m.AirSupply", "int", "", ""}`, `{"m.AirSupply", "int", "", ""}, {"m.AirSupply", "int", "", ""}`, "duplicate source"},
		{"source conflict", `{"m.Sheared", "bool", "", ""}`, `{"m.Sheared", "int", "", ""}`, "conflicting source type"},
		{"source selector", `"m.AirSupply"`, `"m.AirSupply[0]"`, "unsupported source selector"},
		{"source type", `"float64", "float32(m.Health)"`, `"[]float64", "float32(m.Health)"`, "unsupported source type"},
		{"source key", `"float32(m.Health)"`, `"("`, "invalid source expression"},
		{"external revision", `"m.LivingEffects.EffectRevision()", "uint64"`, `"m.LivingEffects.EffectRevision()", "int"`, "external revision requires"},
		{"source coverage", `mobMeta("health", mobMetaSources{{"m.Health", "float64", "float32(m.Health)", ""}}`, `mobMeta("health", nil`, "missing source coverage"},
		{"bad Go body", `s.health = float32(m.Health)`, `s.health = (`, "capture:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(src, test.from) {
				t.Fatal("test replacement did not match schema")
			}
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "internal/server/entity"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "internal/server/mobmeta.go"), []byte(strings.Replace(src, test.from, test.to, 1)), 0644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "internal/server/mobmeta_gen.go")
			sentinel := []byte("leave unchanged")
			if err := os.WriteFile(path, sentinel, 0644); err != nil {
				t.Fatal(err)
			}
			err := runMobMeta(&env{root: root, stdout: io.Discard})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, sentinel) {
				t.Fatal("refused schema overwrote output")
			}
			if _, err := os.Stat(filepath.Join(root, "internal/server/entity/mobmeta_gen.go")); !os.IsNotExist(err) {
				t.Fatalf("refused schema wrote entity output: %v", err)
			}
		})
	}
}

func TestMobMetaCheckDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal/server/entity"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/server/mobmeta.go"), mobMetaInput(t), 0644); err != nil {
		t.Fatal(err)
	}
	linkMobMetaEntitySources(t, root)
	path := filepath.Join(root, "internal/server/mobmeta_gen.go")
	if err := os.WriteFile(path, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	e := &env{root: root, check: true, stdout: io.Discard}
	if err := runMobMeta(e); err != nil {
		t.Fatal(err)
	}
	if len(e.stale) != 2 || e.stale[0] != "internal/server/entity/mobmeta_gen.go" || e.stale[1] != "internal/server/mobmeta_gen.go" {
		t.Fatalf("stale = %v", e.stale)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "stale" {
		t.Fatal("-check wrote output")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/server/entity/mobmeta_gen.go")); !os.IsNotExist(err) {
		t.Fatalf("-check wrote entity output: %v", err)
	}
}

func linkMobMetaEntitySources(t *testing.T, root string) {
	t.Helper()
	sources, err := filepath.Glob(filepath.Join(repoRoot(t), "internal/server/entity/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_gen.go") || strings.HasSuffix(source, "_test.go") {
			continue
		}
		if err := os.Symlink(source, filepath.Join(root, "internal/server/entity", filepath.Base(source))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMobMetaSourceStorageValidation(t *testing.T) {
	groups, err := parseMobMeta(mobMetaInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateMetaSourceTypes(repoRoot(t), groups); err != nil {
		t.Fatal(err)
	}
	for _, source := range []metaSource{
		{path: "m.Horse.missing", typ: "bool"},
		{path: "m.Health", typ: "int"},
		{path: "m.LivingEffects.UnknownRevision()", typ: "uint64"},
	} {
		groups[0].sources = []metaSource{source}
		if err := validateMetaSourceTypes(repoRoot(t), groups); err == nil {
			t.Fatalf("accepted unresolved source %+v", source)
		}
	}
}
