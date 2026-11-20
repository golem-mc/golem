package mobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func writeBiomePack(t *testing.T, levelDir, biomeJSON string) {
	t.Helper()
	pack := filepath.Join(levelDir, "datapacks", "biome_override")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"biome override"}}`,
		"data/minecraft/worldgen/biome/plains.json": biomeJSON,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPackBiomeOverrideControlsNaturalSpawning starts a server with a data
// pack whose minecraft:plains override has an empty monster list and a
// creature list of only cows, then runs natural spawning and chunk-generation
// mob spawning over plains and expects no monsters and only cows.
// The tables are process-wide, so the test is not parallel.
func TestPackBiomeOverrideControlsNaturalSpawning(t *testing.T) {
	t.Cleanup(func() {
		entity.ApplyPackBiomeSpawns(nil)
		registry.ApplyPackBiomeClimates(nil)
		registry.ApplyPackRegistries(nil)
	})
	cfg := servertest.SpawningTestConfig()
	cfg.LevelName = t.TempDir()
	writeBiomePack(t, cfg.LevelName, `{
		"has_precipitation": true,
		"temperature": 0.8,
		"downfall": 0.4,
		"effects": {"water_color": "#3f76e4"},
		"carvers": [],
		"features": [],
		"attributes": {
			"minecraft:gameplay/natural_mob_spawns": {
				"modifier": "overlay",
				"argument": {
					"spawns_by_category": {
						"monster": [],
						"creature": [
							{"type": "minecraft:cow", "weight": 10, "count": 4}
						]
					},
					"spawn_costs": {}
				}
			}
		}
	}`)

	srv, _ := zombieTests(t, cfg)
	srv.SetDayTime(midnight)
	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	if mobs := srv.Mobs(); len(mobs) != 0 {
		t.Errorf("midnight passes spawned %d mobs in overridden plains with empty monster list, want 0", len(mobs))
	}

	layGrass(t, srv, -79, 79, -79, 79)
	srv.SetDayTime(noon)
	for seed := int64(1); seed <= creaturePasses; seed++ {
		srv.SpawnCreaturePass(seed)
	}
	mobs := srv.Mobs()
	if len(mobs) == 0 {
		t.Fatal("creature passes spawned 0 cows in overridden plains, want at least 1")
	}
	for _, m := range mobs {
		if m.Kind != "minecraft:cow" {
			t.Errorf("creature pass spawned %s in overridden plains, want only minecraft:cow", m.Kind)
		}
	}
}

// TestPackBiomeOverrideClimateFormsSnow starts a server on a flat plains world
// with a data pack that makes minecraft:plains snowy (temperature -0.5) and
// verifies that rain forms snow layers and freezes exposed water.
// The tables are process-wide, so the test is not parallel.
func TestPackBiomeOverrideClimateFormsSnow(t *testing.T) {
	t.Cleanup(func() {
		entity.ApplyPackBiomeSpawns(nil)
		registry.ApplyPackBiomeClimates(nil)
		registry.ApplyPackRegistries(nil)
	})
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 2
	cfg.LevelName = t.TempDir()
	writeBiomePack(t, cfg.LevelName, `{
		"has_precipitation": true,
		"temperature": -0.5,
		"downfall": 0.5,
		"effects": {"water_color": "#3f76e4"},
		"carvers": [],
		"features": [],
		"attributes": {}
	}`)

	srv, _ := servertest.Join(t, cfg, "Frosty", servertest.OpsListing(t, "Frosty"))
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 120 {
		srv.TickWeather()
	}
	servertest.SeedPhysics(t, srv, 12345)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 300)

	if !srv.SetBlock(4, 63, 4, registry.BlockWater.DefaultState()) {
		t.Fatal("setup: failed to place water block")
	}
	var snowed, froze bool
	for range 400 {
		srv.TickRandomTicks(1, world.ChunkPos{X: 0, Z: 0})
		if !froze && registry.BlockOf(srv.BlockAt(4, 63, 4)) == registry.BlockIce {
			froze = true
		}
		if !snowed {
			for x := int32(0); x < 16 && !snowed; x++ {
				for z := int32(0); z < 16 && !snowed; z++ {
					if registry.BlockOf(srv.BlockAt(x, 64, z)) == registry.BlockSnow {
						snowed = true
					}
				}
			}
		}
		if snowed && froze {
			break
		}
	}
	if !snowed {
		t.Error("no snow layer formed in plains overridden to temperature -0.5")
	}
	if !froze {
		t.Error("exposed water did not freeze in plains overridden to temperature -0.5")
	}
}
