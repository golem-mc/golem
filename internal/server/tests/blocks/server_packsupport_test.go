package blocks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPackedSoilTagHoldsAFlower checks that a flower's soil is the
// minecraft:supports_vegetation tag in force: a data pack that adds the
// netherite block to it lets a poppy stand on one from startup, and /reload
// with the pack's entry gone refuses it again. The tag tables are
// process-wide, so the test is not parallel and puts the built-in tags back
// when it ends.
// vanilla: VegetationBlock.mayPlaceOn
func TestAPackedSoilTagHoldsAFlower(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "soil")
	soil := filepath.Join(pack, "data", "minecraft", "tags", "block", "supports_vegetation.json")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"soil"}}`)
	write(soil, `{"values":["minecraft:netherite_block"]}`)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Gardener"))
	c.Login("Gardener")
	c.Configure()
	c.EnterWorld("Gardener")

	poppy := servertest.CropsStateOf(t, "minecraft:poppy", nil)
	plant := func(z int32) uint16 {
		t.Helper()
		servertest.SetFarmBlock(t, c, srv, 6, 63, z, "minecraft:netherite_block", nil)
		servertest.GiveFarmItem(t, c, "minecraft:poppy")
		c.UseBlock(6, 63, z)
		c.ExpectBlockAck()
		return srv.BlockAt(6, 64, z)
	}

	if got := plant(0); got != poppy {
		t.Errorf("the poppy on the packed netherite = %d, want %d (the pack's soil holds it)", got, poppy)
	}

	write(soil, `{"values":[]}`)
	c.ChatCommand("reload")
	servertest.AwaitCommandFeedback(t, c, "commands.reload.success")
	if got := plant(2); got != 0 {
		t.Errorf("the poppy on netherite after /reload dropped it = %d, want 0 (refused, air)", got)
	}
}
