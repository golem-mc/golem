package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.push
func TestABystanderCannotPushAnOccupiedCamel(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider, bystander := connect(), connect()
	rider.Join("Rider")
	bystander.Join("Bystander")
	servertest.HoldTicks(t, srv)
	srv.SetWorldAge(1000)
	rider.MoveToGround(2.5, 64, 5.5, true)
	bystander.MoveToGround(8.5, 64, 5.5, true)
	bystander.Settle()
	rider.ChatCommand("summon minecraft:camel 5.5 64 5.5 " + camelSaddled)
	id := rider.AwaitSpawn(camelType(t)).EntityID
	srv.SeedMob(id, 1)
	rider.UseOn(id)
	rider.Settle()
	rider.ReadTicks(srv, 4, func(int32, []byte) bool { return false })
	x, y, z, _ := srv.MobPos(id)
	bystander.MoveToGround(x+0.5, y, z, true)
	bystander.Settle()
	rider.ReadTicks(srv, 4, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlaySetEntityMotion {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				if vx, vy, vz := r.ReadLpVec3(); vx != 0 || vy != 0 || vz != 0 {
					t.Errorf("bystander pushed occupied camel: rider received motion (%g, %g, %g)", vx, vy, vz)
				}
			}
		}
		return false
	})
	if vx, vy, vz, _ := srv.MobVelocity(id); vx != 0 || vy != 0 || vz != 0 {
		t.Errorf("occupied camel velocity = (%g, %g, %g), want zero", vx, vy, vz)
	}
	if gotX, gotY, gotZ, _ := srv.MobPos(id); gotX != x || gotY != y || gotZ != z {
		t.Errorf("idle camel moved to (%g, %g, %g), want (%g, %g, %g)", gotX, gotY, gotZ, x, y, z)
	}
}

// vanilla: LivingEntity.travelRidden, Camel.travel, ServerEntity.sendChanges
func TestMountingAMovingCamelClearsItsMotion(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"), func(s *server.Server) {
		s.SetWorldAge(1000)
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.ChatCommand(`summon minecraft:camel 5.5 64 5.5 {LastPoseTick:0L,Motion:[0.0d,0.0d,-0.5d],equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	id := c.AwaitSpawn(camelType(t)).EntityID
	srv.SeedMob(id, 1)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	x, y, z, _ := srv.MobPos(id)
	if _, _, vz, _ := srv.MobVelocity(id); vz >= 0 || z >= 5.5 {
		t.Fatal("the unmounted camel did not move backwards")
	}
	c.UseOn(id)
	c.Settle()
	stopped := false
	c.ReadTicks(srv, 4, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlaySetEntityMotion {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				vx, vy, vz := r.ReadLpVec3()
				if vx != 0 || vy != 0 || vz != 0 {
					t.Errorf("ridden camel motion = (%g, %g, %g), want zero", vx, vy, vz)
				}
				stopped = true
			}
		}
		return false
	})
	if !stopped {
		t.Error("the rider was not told to clear the camel's pre-mount motion")
	}
	if vx, vy, vz, _ := srv.MobVelocity(id); vx != 0 || vy != 0 || vz != 0 {
		t.Errorf("server retained pre-mount motion (%g, %g, %g)", vx, vy, vz)
	}
	if gotX, gotY, gotZ, _ := srv.MobPos(id); gotX != x || gotY != y || gotZ != z {
		t.Errorf("idle camel moved to (%g, %g, %g), want (%g, %g, %g)", gotX, gotY, gotZ, x, y, z)
	}

	// The client simulates the controls and reports the vehicle's position.
	for _, step := range []struct {
		name  string
		input byte
		dz    float64
	}{
		{"forward", protocol.InputForward, 0.2},
		{"backward", protocol.InputBackward, -0.05},
		{"idle", 0, 0},
	} {
		c.Run(t, step.name, func(t *testing.T) {
			z += step.dz
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(step.input) })
			c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
				b.PutF64(x)
				b.PutF64(y)
				b.PutF64(z)
				b.PutF32(0)
				b.PutF32(0)
				b.PutBool(true)
			})
			c.SettleAndCollect(func(packet int32, _ []byte) {
				if packet == protocol.IDPlayMoveVehicle {
					t.Error("the rider's movement was rejected")
				}
			})
			c.ReadTicks(srv, 4, func(packet int32, data []byte) bool {
				if packet == protocol.IDPlaySetEntityMotion && protocol.NewReader(data).ReadVarInt() == id {
					t.Error("unchanged server motion overwrote the rider's controls")
				}
				return false
			})
			if gotX, gotY, gotZ, _ := srv.MobPos(id); gotX != x || gotY != y || gotZ != z {
				t.Errorf("camel position = (%g, %g, %g), want rider report (%g, %g, %g)", gotX, gotY, gotZ, x, y, z)
			}
		})
	}
}
