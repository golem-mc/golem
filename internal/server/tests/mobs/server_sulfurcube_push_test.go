package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: SulfurCube.playerPush, Entity.playSound
func TestFilledSulfurCubePushSoundHasFixedPitch(t *testing.T) {
	t.Parallel()
	srv, c, _ := sulfurPushWorld(t)
	c.MoveStep(3.25, 64, 4.5, true)
	c.Settle()

	if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data)
		if name != "minecraft:entity.sulfur_cube.explosive.push" {
			return false
		}
		if pitch != 1 {
			t.Errorf("push sound pitch = %g, want 1", pitch)
		}
		return true
	}) {
		t.Fatal("walking into the filled cube played no push sound")
	}
}

// vanilla: Player.aiStep, SulfurCube.playerPush
func TestFilledSulfurCubeIsPushedByRiddenHorse(t *testing.T) {
	t.Parallel()
	srv, c, cube := sulfurPushWorld(t)
	c.ChatCommand(`summon minecraft:horse 6.1 64.75 4.5 {NoAI:1b,NoGravity:1b,Tame:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	c.ChatCommand("ride @s mount @e[type=minecraft:horse,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.Settle()
	servertest.RunTicks(t, srv, 1)

	moveSulfurPusher(c, 5.75, 64.75, 4.5)
	// Even inflated by 0.5, the rider's box is above the 0.98-block cube.
	pos := srv.PlayerPosition("Keeper")
	if pos.X != 5.75 || pos.Y-0.5 <= 64.98 {
		t.Fatalf("rider position = (%g, %g), want the moved seat above the cube", pos.X, pos.Y)
	}
	servertest.RunTicks(t, srv, 2)
	if x, _, _, ok := srv.MobPos(cube); !ok || x >= 4.45 {
		t.Fatalf("cube x = %g, present=%v, want it to move west away from the horse", x, ok)
	}
}

// vanilla: SulfurCube.playerPush, AbstractBoat.getPassengerAttachmentPoint
func TestFilledSulfurCubePushFollowsTheBoatCenter(t *testing.T) {
	t.Parallel()
	srv, c, cube := sulfurPushWorld(t)
	c.ChatCommand(`summon minecraft:oak_chest_boat 6.1 64.75 4.375 {NoGravity:1b}`)
	c.ChatCommand("ride @s mount @e[type=minecraft:oak_chest_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.Settle()
	servertest.RunTicks(t, srv, 1)

	moveSulfurPusher(c, 5.75, 64.75, 4.375)
	// The boat center is north of the cube, and the chest boat's seat is south of it.
	if pos := srv.PlayerPosition("Keeper"); pos.X != 5.75 || pos.Z <= 4.5 {
		t.Fatalf("rider position = (%g, %g), want the moved seat south of the cube", pos.X, pos.Z)
	}
	servertest.RunTicks(t, srv, 2)
	if x, _, z, ok := srv.MobPos(cube); !ok || x >= 4.45 || z <= 4.505 {
		t.Fatalf("cube position = (%g, %g), present=%v, want it to move southwest away from the boat", x, z, ok)
	}
}

func moveSulfurPusher(c *servertest.Client, x, y, z float64) {
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	c.Settle()
}

func sulfurPushWorld(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(3, 64, 4.5, true)
	c.ChatCommand(`summon minecraft:sulfur_cube 4.5 64 4.5 {Age:0,Size:1,PersistenceRequired:1b,equipment:{body:{id:"minecraft:tnt",count:1}}}`)
	c.Settle()
	ids := srv.MobIDsOfKind("minecraft:sulfur_cube")
	if len(ids) != 1 || srv.MobEquipment(ids[0], "body").Empty() {
		t.Fatalf("want one TNT-filled cube, summoned cubes = %v", ids)
	}
	servertest.RunTicks(t, srv, 1)
	return srv, c, ids[0]
}
