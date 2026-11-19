package plugins_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExamplePluginBuilds compiles examples/plugins/hello, a module of its
// own, against this checkout, so the example cannot fall behind the plugin
// API. Its server (cmd/server) is built by `mise run examples:build`.
func TestExamplePluginBuilds(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "plugins", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build in %s: %v\n%s", dir, err, out)
	}
}
