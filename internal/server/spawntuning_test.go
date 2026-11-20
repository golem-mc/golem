package server

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestPluginWorldSpawnTuning checks that the plugin.World spawn limit and
// rate methods act on their own dimension only and ignore a category out of
// range.
func TestPluginWorldSpawnTuning(t *testing.T) {
	t.Parallel()
	a, b := pluginWorld{dim: &dimension{}}, pluginWorld{dim: &dimension{}}
	if a.SpawnLimit(registry.MobCategoryMonster) != -1 || a.TicksPerSpawn(registry.MobCategoryMonster) != -1 {
		t.Fatal("a fresh world is not at the defaults")
	}
	a.SetSpawnLimit(registry.MobCategoryMonster, 3)
	a.SetTicksPerSpawn(registry.MobCategoryMonster, 0)
	if a.SpawnLimit(registry.MobCategoryMonster) != 3 || a.TicksPerSpawn(registry.MobCategoryMonster) != 0 {
		t.Error("the tuned world does not answer what was set")
	}
	if b.SpawnLimit(registry.MobCategoryMonster) != -1 || b.TicksPerSpawn(registry.MobCategoryMonster) != -1 {
		t.Error("tuning one world changed another")
	}
	out := registry.MobCategoryCount + 1 // would wrap onto creature under a modulo
	a.SetSpawnLimit(out, 7)
	a.SetTicksPerSpawn(out, 7)
	if a.SpawnLimit(out) != -1 || a.TicksPerSpawn(out) != -1 {
		t.Error("an out-of-range category answers a value")
	}
	if a.SpawnLimit(registry.MobCategoryCreature) != -1 || a.TicksPerSpawn(registry.MobCategoryCreature) != -1 {
		t.Error("an out-of-range category changed another category")
	}
}
