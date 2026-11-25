package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BeehiveBlock.getDrops, BeehiveBlockEntity.releaseAllOccupants, CopyComponentsFunction.run
func TestABlastCopiesOnlyHiveOccupantsThatDidNotLeave(t *testing.T) {
	for _, tc := range []struct {
		name     string
		retained bool
	}{
		{name: "empty after release"},
		{name: "one occupant cannot leave", retained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			servertest.WritePackFile(t, cfg, "blast", "minecraft/loot_table/blocks/beehive.json", `{
				"type": "minecraft:block",
				"pools": [{"rolls": 1, "entries": [{
					"type": "minecraft:item", "name": "minecraft:beehive", "functions": [{
						"function": "minecraft:copy_components", "source": "block_entity", "include": ["minecraft:bees"]
					}]
				}]}]
			}`)
			srv, _ := servertest.Join(t, cfg, "Watcher")
			servertest.HoldTicks(t, srv)

			occupants := []nbt.Tag{nbt.Compound{
				"entity_data":       nbt.Compound{"id": nbt.String("minecraft:bee")},
				"min_ticks_in_hive": nbt.Int(600),
			}}
			if tc.retained {
				// A valid entity type outside #minecraft:beehive_inhabitors cannot leave.
				occupants = append(occupants, nbt.Compound{
					"entity_data":       nbt.Compound{"id": nbt.String("minecraft:pig")},
					"min_ticks_in_hive": nbt.Int(900),
				})
			}
			hive := registry.BlockBeehive.DefaultState()
			srv.SetBlock(10, 64, 10, hive)
			srv.SetBlockEntityTagAt(10, 64, 10, "minecraft:beehive", nbt.Compound{
				"bees": nbt.List{Elem: nbt.TypeCompound, Items: occupants},
			})
			if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
				t.Fatal("the primed TNT did not spawn")
			}
			servertest.UntilTicks(t, srv, 20, "the blast to destroy the hive", func() bool {
				return srv.BlockAt(10, 64, 10) != hive
			})

			if got := len(srv.MobIDsOfKind("minecraft:bee")); got != 1 {
				t.Errorf("released bees = %d, want 1", got)
			}
			if got := len(srv.MobIDsOfKind("minecraft:pig")); got != 0 {
				t.Errorf("released pigs = %d, want 0", got)
			}
			for _, item := range srv.ItemsInDimension(world.DimensionName) {
				if item.Item != "minecraft:beehive" {
					continue
				}
				bees, _ := item.Stack.BeesOf()
				if !tc.retained {
					if len(bees) != 0 {
						t.Errorf("copied occupants = %+v, want an empty hive", bees)
					}
					return
				}
				pig, _ := registry.EntityTypeID("minecraft:pig")
				if len(bees) != 1 || bees[0].EntityType != pig || bees[0].MinTicksInHive != 900 {
					t.Errorf("copied occupants = %+v, want only the pig occupant with a 900-tick minimum", bees)
				}
				return
			}
			t.Fatal("the blast did not drop the hive")
		})
	}
}
