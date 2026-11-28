package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func standAt(t *testing.T, srv *server.Server, id int32) server.TestArmorStand {
	t.Helper()
	for _, s := range srv.ArmorStandsInDimension(world.DimensionName) {
		if s.ID == id {
			return s
		}
	}
	t.Fatal("the armor stand is gone")
	return server.TestArmorStand{}
}

// An armor stand falling onto a boat or a shulker rests on it.
// vanilla: Entity.collide, AbstractBoat.canBeCollidedWith, Shulker.canBeCollidedWith
func TestArmorStandRestsOnBoatAndShulker(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 0, 0)()
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon oak_boat 2.5 66 2.5 {NoGravity:1b}`)
	srv.RunLine(`summon shulker 8.5 66 2.5 {NoAI:1b}`)
	a := srv.SpawnArmorStand(2.5, 71, 2.5)
	b := srv.SpawnArmorStand(8.5, 71, 2.5)
	servertest.RunTicks(t, srv, 60)
	if y := standAt(t, srv, a).Y; math.Abs(y-66.5625) > 1e-3 {
		t.Errorf("stand over the boat at y=%v, want 66.5625", y)
	}
	if y := standAt(t, srv, b).Y; math.Abs(y-67) > 1e-3 {
		t.Errorf("stand over the shulker at y=%v, want 67", y)
	}
}

// A sliding armor stand stops against a shulker's side.
// vanilla: Entity.collide, Shulker.canBeCollidedWith
func TestArmorStandStopsAtShulker(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 0, 0)()
	servertest.HoldTicks(t, srv)
	srv.RunLine(`setblock 2 63 2 blue_ice`)
	srv.RunLine(`fill 0 63 2 15 63 2 blue_ice`)
	srv.RunLine(`summon shulker 10.5 64 2.5 {NoAI:1b}`)
	srv.RunLine(`summon armor_stand 2.5 64 2.5 {Motion:[1.0d,0d,0d]}`)
	servertest.RunTicks(t, srv, 40)
	var x float64
	for _, s := range srv.ArmorStandsInDimension(world.DimensionName) {
		x = s.X
	}
	if math.Abs(x-9.75) > 0.01 {
		t.Errorf("stand at x=%v, want 9.75", x)
	}
}

// A sliding armor stand pushes a minecart it reaches.
// vanilla: ArmorStand.pushEntities, AbstractMinecart.push
func TestArmorStandPushesMinecart(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	defer srv.HoldChunkInDimension(world.DimensionName, 0, 0)()
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill 0 60 0 24 63 5 blue_ice`)
	srv.RunLine(`summon minecart 6.5 64 2.5 {NoGravity:1b}`)
	srv.RunLine(`summon armor_stand 2.5 64 2.5 {Motion:[1.0d,0d,0d]}`)
	servertest.RunTicks(t, srv, 40)
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 || carts[0].X < 7 {
		t.Errorf("carts %+v, want the cart pushed east of 6.5", carts)
	}
}
