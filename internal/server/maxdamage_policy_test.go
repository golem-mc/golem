package server_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// maxDamageIDOnly lists the callers that hold an item id and no stack.
var maxDamageIDOnly = map[string]bool{
	"internal/server/rules/fishing.go": true, // rolls wear for a stack it is creating
}

// TestStacksReadStackMaxDamage keeps callers that hold a stack on
// registry.StackMaxDamage, so a stack's own max_damage is honoured.
func TestStacksReadStackMaxDamage(t *testing.T) {
	t.Parallel()
	root := filepath.Join(moduleRoot(t), "internal", "server")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(moduleRoot(t), path)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "registry.MaxDamage(") && !maxDamageIDOnly[rel] {
			t.Errorf("%s calls registry.MaxDamage; use registry.StackMaxDamage(stack) or allow it here", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
