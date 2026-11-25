package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BeehiveBlockEntity.releaseOccupant, tickOccupants
func TestPluginCancelledBeeReleaseKeepsOccupant(t *testing.T) {
	t.Parallel()
	attempts := 0
	var listener plugin.Listener
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{
		Name: "keepbee",
		Plugin: &testPlugin{enable: func(s plugin.Server) error {
			listener = plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
				if e.Entity.Type() == registry.EntityTypeBee {
					attempts++
					e.SetCancelled(true)
				}
			})
			return nil
		}},
	}})
	c := servertest.DialServer(t, srv)
	c.JoinAs("Beekeeper")
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(6000)
	const x, y, z = 3, 64, 3
	const bee = "minecraft:bee"
	srv.SetBlock(x, y, z, registry.BlockBeehive.DefaultState())
	srv.SetBlockEntityTagAt(x, y, z, "minecraft:beehive", nbt.Compound{
		"bees": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"entity_data":       nbt.Compound{"id": nbt.String(bee), "NoAI": nbt.Byte(1)},
			"ticks_in_hive":     nbt.Int(601),
			"min_ticks_in_hive": nbt.Int(600),
		}}},
	})
	checkBees := func(wantStored, wantOutside int) {
		t.Helper()
		tag, _ := srv.BlockEntityTagAt(x, y, z)
		occupants, _ := nbt.Get[nbt.List](tag, "bees")
		if stored, outside := len(occupants.Items), len(srv.MobIDsOfKind(bee)); stored != wantStored || outside != wantOutside {
			t.Fatalf("bees stored = %d, outside = %d; want %d stored and %d outside", stored, outside, wantStored, wantOutside)
		}
	}

	servertest.RunTicks(t, srv, 1)
	if attempts != 1 {
		t.Fatalf("spawn attempts = %d, want 1", attempts)
	}
	checkBees(1, 0)

	listener.Unregister()
	servertest.RunTicks(t, srv, 1)
	checkBees(0, 1)
	servertest.RunTicks(t, srv, 3)
	checkBees(0, 1)
}
