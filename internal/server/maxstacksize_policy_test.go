package server_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// maxStackSizeByIDAllowed lists the callers with only an item id, no stack.
var maxStackSizeByIDAllowed = map[string]bool{
	"internal/server/entity/villagerfood.go":   true,
	"internal/server/inventory/merchant.go":    true,
	"internal/server/inventory/offers_draw.go": true,
	"internal/server/inventory/recipebook.go":  true,
	"internal/server/rules/loot.go":            true,
}

// TestMaxStackSizeByIDStaysWithIDOnlyCallers keeps stack holders on
// registry.StackMaxStackSize, vanilla's ItemStack.getMaxStackSize.
func TestMaxStackSizeByIDStaysWithIDOnlyCallers(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal", "server"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.Contains(string(src), "registry.MaxStackSize(") && !maxStackSizeByIDAllowed[rel] {
			t.Errorf("%s calls registry.MaxStackSize; use registry.StackMaxStackSize(stack), or allowlist an id-only caller", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
