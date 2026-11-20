package mobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const packFrostyWolf = `{
  "assets": {
    "angry": "minecraft:entity/wolf/wolf_snowy_angry",
    "tame": "minecraft:entity/wolf/wolf_snowy_tame",
    "wild": "minecraft:entity/wolf/wolf_snowy"
  },
  "baby_assets": {
    "angry": "minecraft:entity/wolf/wolf_snowy_angry_baby",
    "tame": "minecraft:entity/wolf/wolf_snowy_tame_baby",
    "wild": "minecraft:entity/wolf/wolf_snowy_baby"
  },
  "spawn_conditions": [
    {
      "condition": {
        "type": "minecraft:biome",
        "biomes": "minecraft:plains"
      },
      "priority": 5
    }
  ]
}`

const packColdPigInPlains = `{
  "asset_id": "minecraft:entity/pig/pig_cold",
  "baby_asset_id": "minecraft:entity/pig/pig_cold_baby",
  "model": "cold",
  "spawn_conditions": [
    {
      "condition": {
        "type": "minecraft:biome",
        "biomes": "minecraft:plains"
      },
      "priority": 5
    }
  ]
}`

// TestDataPackVariantSpawnConditions: a data pack adding test:frosty to
// wolf_variant and overriding minecraft:pig_variant/cold at priority 5 in
// plains makes finalized spawns in a plains column pick those variants.
// Process-wide registry state, so the test is not parallel.
// vanilla: VariantUtils.selectVariantToSpawn
func TestDataPackVariantSpawnConditions(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "variants")
	for file, body := range map[string]string{
		"pack.mcmeta":                          `{"pack":{"description":"variant spawns"}}`,
		"data/test/wolf_variant/frosty.json":   packFrostyWolf,
		"data/minecraft/pig_variant/cold.json": packColdPigInPlains,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srv, c := servertest.Join(t, cfg, "Breeder", servertest.OpsListing(t, "Breeder"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:wolf 4.5 64 7.5")
	c.ChatCommand("summon minecraft:pig 6.5 64 7.5")
	c.SettleAndCollect(func(int32, []byte) {})

	if got := savedField(t, srv, "minecraft:wolf", "variant"); got != `"test:frosty"` {
		t.Errorf("plains wolf variant = %s, want \"test:frosty\"", got)
	}
	if got := savedField(t, srv, "minecraft:pig", "variant"); got != `"minecraft:cold"` {
		t.Errorf("plains pig variant = %s, want \"minecraft:cold\"", got)
	}
}
