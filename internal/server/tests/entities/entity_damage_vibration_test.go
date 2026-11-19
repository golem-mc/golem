package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func sensorPhase(t *testing.T, srv *server.Server) string {
	t.Helper()
	_, props := servertest.StatePropsOf(t, srv, 4, 64, 2)
	return props["sculk_sensor_phase"]
}

// TestHurtingABodyPostsAnEntityDamageVibration: a survival punch on a boat,
// a minecart or an armor stand, and an item burning in lava, set off a sculk
// sensor three blocks away.
// vanilla: VehicleEntity.hurtServer, ArmorStand.hurtServer, ItemEntity.hurtServer
func TestHurtingABodyPostsAnEntityDamageVibration(t *testing.T) {
	t.Parallel()
	bodies := map[string]func(srv *server.Server) int32{
		"boat":       func(srv *server.Server) int32 { return srv.SpawnBoat(0.5, 64, 2.5, "minecraft:oak_boat") },
		"minecart":   func(srv *server.Server) int32 { return srv.SpawnMinecart(0.5, 64, 2.5) },
		"armorstand": func(srv *server.Server) int32 { return srv.SpawnArmorStand(0.5, 64, 2.5) },
	}
	for name, spawn := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, "survival", "Puncher", 0)
			srv.SetBlock(4, 64, 2, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			id := spawn(srv)
			if id == 0 {
				t.Fatal("spawn came to nothing")
			}
			c.Settle()
			if phase := sensorPhase(t, srv); phase != "inactive" {
				t.Fatalf("sensor phase = %q before the punch, want inactive", phase)
			}
			servertest.AwaitFullSwing(t, srv, "Puncher")
			c.Attack(id)
			servertest.UntilTicks(t, srv, 10, "the sensor hears the punch", func() bool {
				return sensorPhase(t, srv) == "active"
			})
		})
	}
}

// TestAnItemBurningInLavaPostsAnEntityDamageVibration: a stack on a lava cell
// hurts each tick, which a sculk sensor beside it hears.
// vanilla: ItemEntity.hurtServer
func TestAnItemBurningInLavaPostsAnEntityDamageVibration(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Dropper", 0)
	srv.SetBlock(4, 64, 2, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	srv.SetBlock(1, 64, 2, servertest.MustState(t, "minecraft:lava", map[string]string{"level": "0"}))
	c.Settle()
	srv.SpawnItemEntity(1.5, 64.5, 2.5, "minecraft:stick", 1)
	servertest.UntilTicks(t, srv, 20, "the sensor hears the burning stack", func() bool {
		return sensorPhase(t, srv) == "active"
	})
}
