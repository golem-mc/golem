package items

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A tool tag does not grant a block_transformer component to bone meal.
// vanilla: Item.useOn, BoneMealItem.useOn
func TestABoneMealInTheHoesTagGrowsWithoutTilling(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "hoemeal")
	hoes := filepath.Join(pack, "data", "minecraft", "tags", "item", "hoes.json")
	files := map[string]string{
		filepath.Join(pack, "pack.mcmeta"): `{"pack":{"description":"hoemeal"}}`,
		hoes:                               `{"values":["minecraft:bone_meal"]}`,
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Camper", servertest.OpsListing(t, "Camper"))

	servertest.HoldTicks(t, srv)

	grass := registry.BlockStates["minecraft:grass_block"]
	srv.SetBlock(3, 63, 0, grass)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:air"])
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")
	c.UseBlock(3, 63, 0)
	c.Settle()

	if got := srv.BlockAt(3, 63, 0); got != grass {
		t.Errorf("the grass under the tagged bone meal = %d, want %d (grass)", got, grass)
	}
}
