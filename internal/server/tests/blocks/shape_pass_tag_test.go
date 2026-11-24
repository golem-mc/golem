package blocks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// shapePackTags runs a server whose data pack lists values in
// minecraft:fences, and returns a check that placing stone east of a
// brown_mushroom_block at the given z connects the mushroom's east face the
// way the full shape pass does. The tag tables are process-wide, so these
// tests are not parallel.
func shapePackTags(t *testing.T, startup string) (reload func(values string), check func(z int32)) {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "fences")
	fences := filepath.Join(pack, "data", "minecraft", "tags", "block", "fences.json")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"fences"}}`)
	write(fences, startup)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Fencer"))
	c.Login("Fencer")
	c.Configure()
	c.EnterWorld("Fencer")
	reload = func(values string) {
		write(fences, values)
		c.ChatCommand("reload")
		servertest.AwaitCommandFeedback(t, c, "commands.reload.success")
	}
	check = func(z int32) {
		t.Helper()
		servertest.Setblock(t, c, 5, 64, z, "brown_mushroom_block[east=false]")
		servertest.Setblock(t, c, 6, 64, z, "stone")
		c.Settle()
		got := registry.StateBool(srv.BlockAt(5, 64, z), registry.PropEast)
		if !got {
			t.Errorf("mushroom at z=%d: east face is clear beside stone, want the shape pass to connect it", z)
		}
	}
	return reload, check
}

// TestAPackedFenceTagRunsTheShapePassFromStartup checks that a block a data
// pack adds to minecraft:fences gets its shape writes although the built-in
// tables count no such block.
// vanilla: Level.neighborShapeChanged
func TestAPackedFenceTagRunsTheShapePassFromStartup(t *testing.T) {
	_, check := shapePackTags(t, `{"values":["minecraft:brown_mushroom_block"]}`)
	check(0)
}

// TestAReloadedFenceTagRunsTheShapePass is the same after /reload adds the member.
func TestAReloadedFenceTagRunsTheShapePass(t *testing.T) {
	reload, check := shapePackTags(t, `{"values":[]}`)
	reload(`{"values":["minecraft:brown_mushroom_block"]}`)
	check(2)
}
