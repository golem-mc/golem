package server_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// vanillaFiller matches the prose the comment rules ban: a comment says what
// the code does, and "vanilla: Class.method" is where a source is cited. The
// pattern is split so this file does not match itself.
var vanillaFiller = regexp.MustCompile(
	"as vanilla " + "does" +
		"|the way (a )?" + "vanilla" +
		"|as in " + "vanilla" +
		"|mirrors " + "vanilla")

// TestNoVanillaFillerInComments holds the comment rule that a vanilla source
// is a short "vanilla:" tag, not a retelling. It walks the packages the rule
// covers, test files included.
func TestNoVanillaFillerInComments(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for _, dir := range []string{"internal/server", "registry", "nbt"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				if vanillaFiller.MatchString(line) {
					rel, relErr := filepath.Rel(root, path)
					if relErr != nil {
						rel = path
					}
					t.Errorf("%s:%d: vanilla prose in a comment: %q", rel, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", dir, walkErr)
		}
	}
}

// moduleRoot finds the directory holding go.mod above the test's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
