package explosions

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockBehaviour.onExplosionHit
func TestABlastRollsEachBlocksLootAtItsOwnOrigin(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "blast", "minecraft/loot_table/blocks/glass.json", `{
		"pools": [{"rolls": 1, "entries": [{"type": "minecraft:alternatives", "children": [
			{"type": "minecraft:item", "name": "minecraft:diamond", "condition":
				{"type": "minecraft:location_check", "predicate": {"position": {"x": 10.5}}}
			},
			{"type": "minecraft:item", "name": "minecraft:emerald", "condition":
				{"type": "minecraft:location_check", "predicate": {"position": {"x": 12.5}}}
			}
		]}]}]
	}`)
	srv, _ := servertest.Join(t, cfg, "Watcher")
	srv.SetGameRule("tnt_explosion_drop_decay", false)
	glass, ok := registry.FindState("minecraft:glass", nil)
	if !ok {
		t.Fatal("the block registry has no glass")
	}
	servertest.HoldTicks(t, srv)
	srv.SetBlock(10, 64, 10, glass)
	srv.SetBlock(12, 64, 10, glass)
	if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}
	servertest.UntilTicks(t, srv, 20, "both glass blocks to break", func() bool {
		return srv.BlockAt(10, 64, 10) != glass && srv.BlockAt(12, 64, 10) != glass
	})
	got := map[string]int32{}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:diamond" || it.Item == "minecraft:emerald" {
			got[it.Item] += int32(it.Count)
		}
	}
	want := map[string]int32{"minecraft:diamond": 1, "minecraft:emerald": 1}
	if !maps.Equal(got, want) {
		t.Errorf("glass drops = %v, want %v", got, want)
	}
}
