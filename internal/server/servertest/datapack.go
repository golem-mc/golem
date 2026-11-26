package servertest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// PackEnchantments writes the enchantment files (by path under data/) into a
// data pack of a fresh level directory and returns it. The registry tables are
// process-wide, so the tests using it are not parallel and put them back.
func PackEnchantments(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "p")
	files["pack.mcmeta"] = `{"pack":{"description":"p"}}`
	for file, body := range files {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
