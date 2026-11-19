package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// notDirected are the generators `go generate` leaves out, with the reason.
var notDirected = map[string]string{
	"registries": "needs a registry dump from a running vanilla server (tools/probe)",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := vanilla.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

var directive = regexp.MustCompile(`(?m)^//go:generate go run (?:\.\./)+tools/gen ([a-z0-9 -]+)$`)

// directedNames collects the generator names the //go:generate lines under
// the repository name, with the file each sits in.
func directedNames(t *testing.T, root string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (rel == "tools" || rel == ".git" || rel == "reference-server" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range directive.FindAllStringSubmatch(string(src), -1) {
			for name := range strings.FieldsSeq(m[1]) {
				if other, dup := found[name]; dup {
					t.Errorf("%s is directed by both %s and %s", name, other, rel)
				}
				found[name] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// Every generator is reachable from `go generate ./...`, or says why not, and
// every directive names a generator that exists.
func TestEveryGeneratorHasADirective(t *testing.T) {
	directed := directedNames(t, repoRoot(t))
	for _, name := range sortedNames() {
		_, ok := directed[name]
		reason, excused := notDirected[name]
		switch {
		case !ok && !excused:
			t.Errorf("generator %s has no //go:generate line (add one, or list it in notDirected with the reason)", name)
		case ok && excused:
			t.Errorf("generator %s is in notDirected (%s) but has a //go:generate line", name, reason)
		}
	}
	for name, file := range directed {
		if _, ok := generators[name]; !ok {
			t.Errorf("%s directs %s, which is not a generator", file, name)
		}
	}
}

var misePrefix = regexp.MustCompile(`(?m)^\[tasks\."gen:([a-z0-9-]+)"\]\n(?:[^\[\n][^\n]*\n)*?run = "go run \./tools/gen ([a-z0-9-]+)"$`)

// Every generator has a gen:<name> mise task that runs it.
func TestEveryGeneratorHasAMiseTask(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var tasks []string
	for _, m := range misePrefix.FindAllStringSubmatch(string(src), -1) {
		if m[1] != m[2] {
			t.Errorf("task gen:%s runs generator %s", m[1], m[2])
		}
		tasks = append(tasks, m[1])
	}
	for _, name := range sortedNames() {
		if !slices.Contains(tasks, name) {
			t.Errorf("generator %s has no gen:%s task in mise.toml", name, name)
		}
	}
}
