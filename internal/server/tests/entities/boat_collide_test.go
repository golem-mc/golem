package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// boatCollideRig joins an operator over a floor of the block, y 99, wide
// enough for a boat sliding east.
func boatCollideRig(t *testing.T, floor registry.Block) (*server.Server, *servertest.Client) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Bumper", servertest.OpsListing(t, "Bumper"))
	servertest.HoldTicks(t, srv)
	for x := int32(-2); x <= 50; x++ {
		for z := int32(-1); z <= 3; z++ {
			srv.SetBlock(x, 99, z, floor.DefaultState())
		}
	}
	return srv, c
}

// summonAwait summons the command's entity and returns its ID.
func summonAwait(c *servertest.Client, cmd string, typ int32) int32 {
	c.ChatCommand(cmd)
	return c.AwaitSpawn(typ).EntityID
}

// An unridden hull's move stops at another hull and at a cart.
// vanilla: AbstractBoat.canCollideWith, Entity.canVehicleCollide, Entity.collide
func TestUnriddenBoatCollidesWithHullsAndCarts(t *testing.T) {
	t.Parallel()
	t.Run("hull", func(t *testing.T) {
		t.Parallel()
		srv, c := boatCollideRig(t, registry.BlockBlueIce)
		a := summonAwait(c, "summon oak_boat 2.5 100 0.5 {Motion:[1.2d,0d,0d]}", protocol.EntityTypeOakBoat)
		b := summonAwait(c, "summon oak_boat 9.5 100 0.5", protocol.EntityTypeOakBoat)
		servertest.RunTicks(t, srv, 40)
		ax, _, _, _, _, _ := srv.BoatState(a)
		bx, _, _, _, _, _ := srv.BoatState(b)
		if ax >= bx-1.375 || bx <= 9.5 {
			t.Errorf("A at x=%v, B at x=%v: want A west of B's box and B shoved east (vanilla 3.83, 13.86)", ax, bx)
		}
	})
	t.Run("cart", func(t *testing.T) {
		t.Parallel()
		srv, c := boatCollideRig(t, registry.BlockBlueIce)
		a := summonAwait(c, "summon oak_boat 2.5 100 0.5 {Motion:[1.2d,0d,0d]}", protocol.EntityTypeOakBoat)
		cart := summonAwait(c, "summon minecart 9.5 100 0.5", protocol.EntityTypeMinecart)
		servertest.RunTicks(t, srv, 40)
		ax, _, _, _, _, _ := srv.BoatState(a)
		cx, _, _, _, _ := srv.MinecartState(cart)
		if ax >= cx-0.49-0.6875+1e-6 {
			t.Errorf("A at x=%v passed the cart at x=%v (vanilla 5.56)", ax, cx)
		}
	})
	for _, k := range []struct {
		kind string
		typ  int32
	}{{"oak_boat", protocol.EntityTypeOakBoat}, {"oak_chest_boat", protocol.EntityTypeOakChestBoat}, {"bamboo_raft", protocol.EntityTypeBambooRaft}} {
		t.Run("onto "+k.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := boatCollideRig(t, registry.BlockStone)
			low := summonAwait(c, fmt.Sprintf("summon %s 5.5 105 0.5 {NoGravity:1b}", k.kind), k.typ)
			up := summonAwait(c, "summon oak_boat 5.5 110 0.5", protocol.EntityTypeOakBoat)
			servertest.RunTicks(t, srv, 60)
			_, ly, _, _, _, _ := srv.BoatState(low)
			_, y, _, _, _, _ := srv.BoatState(up)
			if y < ly+0.5625-0.01 || y > ly+0.5625+0.01 {
				t.Errorf("upper boat y=%v, lower %v: want rest at lower+0.5625", y, ly)
			}
		})
	}
	t.Run("onto minecart", func(t *testing.T) {
		t.Parallel()
		srv, c := boatCollideRig(t, registry.BlockStone)
		cart := summonAwait(c, "summon minecart 5.5 105 0.5 {NoGravity:1b}", protocol.EntityTypeMinecart)
		b := summonAwait(c, "summon oak_boat 5.5 110 0.5", protocol.EntityTypeOakBoat)
		servertest.RunTicks(t, srv, 60)
		_, cy, _, _, _ := srv.MinecartState(cart)
		_, y, _, _, _, _ := srv.BoatState(b)
		if y < cy+0.7-0.01 || y > cy+0.7+0.01 {
			t.Errorf("boat y=%v, cart %v: want rest at cart+0.7", y, cy)
		}
	})
}
