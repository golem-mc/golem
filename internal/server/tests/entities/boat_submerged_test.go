package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractBoat.tick, floatBoat
func TestSummonedSubmergedBoatStaysOnBottom(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Diver", servertest.OpsListing(t, "Diver"))
	servertest.HoldTicks(t, srv)
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(x, 99, z, registry.BlockStone.DefaultState())
			srv.SetBlock(x, 100, z, registry.BlockWater.DefaultState())
		}
	}
	c.ChatCommand("summon oak_boat 0.5 100 0.5")
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	servertest.RunTicks(t, srv, 40)
	x, y, z, _, grounded, ok := srv.BoatState(boat)
	if !ok || x != 0.5 || y != 100 || z != 0.5 || !grounded {
		t.Fatalf("submerged boat pos=(%g,%g,%g) grounded=%v exists=%v; want (0.5,100,0.5) on bottom", x, y, z, grounded, ok)
	}
}
