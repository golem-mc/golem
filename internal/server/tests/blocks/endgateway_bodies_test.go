package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func gatewayBody(t *testing.T, srv *server.Server, kind string) command.Entity {
	t.Helper()
	bodies := srv.SelectedEntities(kind)
	if len(bodies) != 1 {
		t.Fatalf("%s bodies = %d, want one", kind, len(bodies))
	}
	return bodies[0]
}

func gatewayBodyTag(t *testing.T, body command.Entity) nbt.Compound {
	t.Helper()
	tag, ok := body.(command.DataTarget).DataTag()
	if !ok {
		t.Fatal("body has no saved data")
	}
	return tag.Entries()
}

func gatewayBodyWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	servertest.HoldTicks(t, srv)
	srv.SetLoadedBlock(3, gatewayY-1, 3, servertest.BlockState(t, "minecraft:stone"))
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 119, 20, servertest.BlockState(t, "minecraft:stone"))
	return srv, c
}

func TestEndGatewayCarriesEmptyVehicles(t *testing.T) {
	for _, kind := range []string{"minecraft:oak_boat", "minecraft:minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := gatewayBodyWorld(t)
			if kind == "minecraft:oak_boat" {
				srv.SpawnBoat(3.5, gatewayY, 3.5, kind)
			} else {
				srv.SpawnMinecart(3.5, gatewayY, 3.5)
			}
			servertest.UntilTicks(t, srv, 100, "empty vehicle at gateway exit", func() bool {
				srv.WaitGatewayMoves() // the landing search runs off the tick
				p := gatewayBody(t, srv, kind).Position()
				return p.X == 20.5 && p.Y == 120 && p.Z == 20.5
			})
			if cooldown, _ := nbt.IntField(gatewayBodyTag(t, gatewayBody(t, srv, kind)), "PortalCooldown"); cooldown <= 0 || cooldown > 10 {
				// vanilla: VehicleEntity.getDimensionChangingDelay
				t.Fatalf("portal cooldown = %v, want 1..10", cooldown)
			}
			// A different gateway at the arrival must not bounce the body back.
			srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:end_gateway"))
			srv.SetEndGatewayExit(20, 120, 20, 3, gatewayY, 3, true)
			servertest.RunTicks(t, srv, 5)
			srv.WaitGatewayMoves()
			if p := gatewayBody(t, srv, kind).Position(); p.X != 20.5 || p.Z != 20.5 {
				t.Fatalf("cooling vehicle bounced to %+v", p)
			}
		})
	}
}

func TestEndGatewayCarriesFirework(t *testing.T) {
	t.Parallel()
	srv, _ := gatewayBodyWorld(t)
	// Its centre skips the cell in one tick; the swept box still touches it.
	srv.SpawnFireworkAtAngle(2.5, gatewayY+0.5, 3.5, 2, 0, 0, 10000)
	servertest.UntilTicks(t, srv, 100, "rocket at gateway exit", func() bool {
		srv.WaitGatewayMoves() // the landing search runs off the tick
		return gatewayBody(t, srv, "minecraft:firework_rocket").Position().Z == 20.5
	})
	tag := gatewayBodyTag(t, gatewayBody(t, srv, "minecraft:firework_rocket"))
	motion := tag["Motion"].(nbt.List)
	if motion.Items[0] != nbt.Double(2) {
		t.Fatalf("rocket motion = %v, want its original x velocity 2", motion)
	}
	if cooldown, _ := nbt.IntField(tag, "PortalCooldown"); cooldown <= 0 || cooldown > 2 {
		// vanilla: Projectile.getDimensionChangingDelay
		t.Fatalf("rocket portal cooldown = %d, want 1..2", cooldown)
	}
}

func TestEndGatewayCarriesArmorStand(t *testing.T) {
	t.Parallel()
	srv, _ := gatewayBodyWorld(t)
	// Only its head overlaps the gateway.
	srv.SetLoadedBlock(3, gatewayY-1, 3, 0)
	srv.SpawnArmorStand(3.5, gatewayY-1, 3.5)
	servertest.UntilTicks(t, srv, 100, "stand at gateway exit", func() bool {
		return gatewayBody(t, srv, "minecraft:armor_stand").Position().Z == 20.5
	})
	_, vy, _, ok := srv.ArmorStandVelocity(gatewayBody(t, srv, "minecraft:armor_stand").EntityID())
	if !ok || vy >= 0 {
		t.Fatalf("stand lost its fall: vy=%v, present=%v", vy, ok)
	}
}
