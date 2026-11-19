package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestMobCrammedAmongVehiclesTakesCrammingDamage: a held mob sharing its box
// with three minecarts or three boats, past a limit of two, takes the
// cramming hurt; boats and carts are pushable and count. Among boats it is an
// iron golem, wider than a hull, so no boat takes it aboard.
// vanilla: LivingEntity.pushEntities, AbstractBoat.isPushable,
// AbstractMinecart.isPushable
func TestMobCrammedAmongVehiclesTakesCrammingDamage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ kind, mob string }{
		{"minecart", "minecraft:pig"},
		{"boat", "minecraft:iron_golem"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			srv.SetGameRuleValue("max_entity_cramming", 2)
			// Every body on one spot: under Entity.push's deadband, so none
			// is pushed out of the crowd.
			const x, z = 4.5, 0.5
			mob := srv.SpawnMob(tc.mob, x, 64, z)
			if !srv.SetMobNoAI(mob) {
				t.Fatal("the mob could not be held")
			}
			for range 3 {
				if tc.kind == "boat" {
					srv.SpawnBoat(x, 64, z, "minecraft:oak_boat")
				} else {
					srv.SpawnMinecart(x, 64, z)
				}
			}
			servertest.RunTicks(t, srv, 60)
			want := registry.MustID("minecraft:damage_type", "minecraft:cramming")
			hit := false
			c.SettleAndCollect(func(pid int32, data []byte) {
				if pid != protocol.IDPlayDamageEvent {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() == mob && r.ReadVarInt() == want {
					hit = true
				}
			})
			if !hit {
				t.Error("no cramming damage event for the mob")
			}
		})
	}
}

// vanilla: Entity.rideTick, LivingEntity.pushEntities
func TestBoatPassengerTakesCrammingDamage(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetGameRuleValue("max_entity_cramming", 2)
	const x, z = 4.5, 0.5
	pig := srv.SpawnMob("minecraft:pig", x, 64, z)
	if !srv.SetMobNoAI(pig) {
		t.Fatal("the pig could not be held")
	}
	boat := srv.SpawnBoat(x, 64, z, "minecraft:oak_boat")
	if !srv.MountMobOnBoat(pig, boat) {
		t.Fatal("the pig could not board")
	}
	for range 2 {
		srv.SpawnBoat(x, 64, z, "minecraft:oak_boat")
	}
	servertest.RunTicks(t, srv, 60)
	want := registry.MustID("minecraft:damage_type", "minecraft:cramming")
	hit := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayDamageEvent {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == pig && r.ReadVarInt() == want {
				hit = true
			}
		}
	})
	if !hit {
		t.Error("no cramming damage event for the boat passenger after 60 ticks")
	}
}
