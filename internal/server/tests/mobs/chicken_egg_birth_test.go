package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Chicken.aiStep, Entity.spawnAtLocation
func TestChickenLaysEggAtItsFeet(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Farmer", servertest.OpsListing(t, "Farmer"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon chicken 4.5 64 4.5 {NoAI:1b,EggLayTime:1}`)
	hen := c.AwaitSpawn(protocol.EntityTypeChicken)
	servertest.RunTicks(t, srv, 1)
	egg := c.AwaitSpawn(protocol.EntityTypeItem)
	if egg.X != hen.X || egg.Y != hen.Y || egg.Z != hen.Z {
		t.Errorf("egg spawn = (%g, %g, %g), want hen's feet (%g, %g, %g)", egg.X, egg.Y, egg.Z, hen.X, hen.Y, hen.Z)
	}
	if y := solidEntityY(t, srv, "@e[type=minecraft:item,limit=1]"); y != hen.Y {
		t.Errorf("egg NBT feet Y = %g, want hen's feet %g", y, hen.Y)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || registry.Item(items[0].Stack.ItemID) != registry.ItemEgg || items[0].Age != 0 || items[0].Health != 5 {
		t.Fatalf("birth tick = %+v, want one unstepped, healthy egg", items)
	}
}
