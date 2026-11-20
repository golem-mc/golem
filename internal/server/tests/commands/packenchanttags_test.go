package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackRandomLootTagLimitsDungeonBooks checks that a dungeon chest's
// enchant_randomly reads on_random_loot from the loaded tags: with a pack
// replacing it by unbreaking, every enchanted book holds only unbreaking.
// vanilla: EnchantRandomlyFunction.run, RegistryCodecs.homogeneousList
func TestPackRandomLootTagLimitsDungeonBooks(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	unbreaking, _ := registry.EnchantmentID("minecraft:unbreaking")
	cfg := servertest.ItemCreativeConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/tags/enchantment/on_random_loot.json": `{"replace":true,"values":["minecraft:unbreaking"]}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	c.ChatCommand("setblock 2 64 3 minecraft:barrel")
	c.Settle()
	servertest.RunTicks(t, srv, 2)

	books := 0
	for range 300 {
		c.ChatCommand("loot replace block 2 64 3 container.0 27 loot minecraft:chests/simple_dungeon")
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		for _, s := range srv.InventoryAt(2, 64, 3) {
			if len(s.StoredEnchantments) == 0 {
				continue
			}
			books++
			for id := range s.StoredEnchantments {
				if id != unbreaking {
					t.Fatalf("enchanted book holds enchantment %d, want only unbreaking", id)
				}
			}
		}
		if books >= 3 {
			break
		}
	}
	if books == 0 {
		t.Fatal("no enchanted book in 300 dungeon rolls")
	}
	c.Disconnect()
}
