package plugins_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// TestFunctionSeesPluginCommandChanges checks that a function runs a plugin
// command overriding a vanilla one once it is registered, and falls back to
// vanilla's once it is unregistered or the sender's permission for it is
// removed, though the command tree is cached between lines.
func TestFunctionSeesPluginCommandChanges(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "test")
	for name, body := range map[string]string{
		"pack.mcmeta":                  `{"pack":{"pack_format":101,"description":"test"}}`,
		"data/t/function/f.mcfunction": "list\n",
	} {
		path := filepath.Join(pack, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "Warps", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		h = s
		return nil
	}}}})
	var runs atomic.Int32
	var allowed atomic.Bool
	allowed.Store(true)
	run := func(f func()) {
		h.Scheduler().RunTask(f)
		servertest.RunTicks(t, srv, 1)
	}
	call := func() int32 {
		before := runs.Load()
		srv.RunLine("function t:f")
		servertest.RunTicks(t, srv, 2)
		return runs.Load() - before
	}
	run(func() {
		plugin.Provide[plugin.PermissionProvider](h, permissionProvider(func(_ plugin.CommandSender, node string) (bool, bool) {
			if node == "warps.use" {
				return allowed.Load(), true
			}
			return false, false
		}), 1)
	})
	if n := call(); n != 0 {
		t.Fatalf("unregistered command ran %d times", n)
	}
	run(func() {
		err := h.Commands().Register(plugin.Command{Name: "list", Override: true, Permission: "warps.use", Node: plugin.Node{Run: func(*plugin.CommandContext) (int, error) {
			runs.Add(1)
			return 1, nil
		}}})
		if err != nil {
			t.Error(err)
		}
	})
	if n := call(); n != 1 {
		t.Fatalf("registered command ran %d times", n)
	}
	if n := call(); n != 1 {
		t.Fatalf("second call ran %d times", n)
	}
	allowed.Store(false)
	if n := call(); n != 0 {
		t.Fatalf("command without permission ran %d times", n)
	}
	allowed.Store(true)
	if n := call(); n != 1 {
		t.Fatalf("command with permission back ran %d times", n)
	}
	run(func() { h.Commands().Unregister("list") })
	if n := call(); n != 0 {
		t.Fatalf("unregistered command ran %d times", n)
	}
}
