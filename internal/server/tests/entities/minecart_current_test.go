package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWaterCurrentCarriesAMinecartOffItsRails: a cart with no rail in a
// flowing 1-wide channel is pushed downstream like any other body.
// vanilla: AbstractMinecart.tick, Entity.updateFluidInteraction
func TestAWaterCurrentCarriesAMinecartOffItsRails(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Drifter"))
	c := connect().Join("Drifter")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	const x, y, z = 20, 64, 10
	glass := servertest.MustState(t, "minecraft:glass", nil)
	for _, cmd := range []struct {
		text    string
		x, y, z int32
		block   uint16
	}{
		{"fill 19 63 9 21 67 22 minecraft:glass", 21, 67, 22, glass},
		{"fill 20 64 10 20 67 21 minecraft:air", 20, 67, 21, 0},
		{"setblock 20 64 10 minecraft:water", 20, 64, 10, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"})},
	} {
		c.ChatCommand(cmd.text)
		c.Settle()
		servertest.UntilTicks(t, srv, 50, cmd.text, func() bool { return srv.BlockAt(cmd.x, cmd.y, cmd.z) == cmd.block })
	}
	servertest.RunTicks(t, srv, 200)
	cart := srv.SpawnMinecart(x+0.5, y, z+2.5)
	servertest.RunTicks(t, srv, 5)
	vz, ok := srv.MinecartVelocityZ(cart)
	if !ok {
		t.Fatal("the cart is gone")
	}
	if d := vz - 0.027125; d > 1e-5 || d < -1e-5 {
		t.Errorf("Motion[2] after 5 ticks = %v, want 0.027125 (vanilla 26.3)", vz)
	}
	servertest.RunTicks(t, srv, 15)
	if _, _, pz, _, _ := srv.MinecartState(cart); pz <= z+2.5+0.2 {
		t.Errorf("Pos[2] after 20 ticks = %v, want > %v", pz, z+2.7)
	}
}
