package main

import (
	"bytes"
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
