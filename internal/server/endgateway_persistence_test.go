package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func TestGatewayBodiesKeepPortalCooldownInTheirSave(t *testing.T) {
	s := &Server{}
	for _, kind := range []string{"minecraft:oak_boat", "minecraft:minecart", armorStandEntityID, fireworkEntityID} {
		t.Run(kind, func(t *testing.T) {
			tag := entityBaseTag(kind, 3.5, 90, 3.5, 0.2, -0.3, 0.4, 30, 10, uuid.New(), false, 123, 0, false)
			var saved nbt.Compound
			switch kind {
			case "minecraft:oak_boat":
				saved = boatTag(boatOfTag(s, nil, tag)).Entries()
			case "minecraft:minecart":
				saved = minecartTag(minecartOfTag(s, nil, tag)).Entries()
			case armorStandEntityID:
				saved = armorStandTag(armorStandOfTag(s, nil, tag)).Entries()
			case fireworkEntityID:
				saved = fireworkTag(s.fireworkOfTag(nil, tag)).Entries()
			}
			if saved["PortalCooldown"] != nbt.Int(123) {
				t.Fatalf("saved cooldown = %v, want 123", saved["PortalCooldown"])
			}
		})
	}
}

func TestGatewayMovesKeepMotionAndFacing(t *testing.T) {
	s := &Server{}
	boat := &boatEntity{boat: entity.NewBoat(1, 3, 90, 3, 30)}
	boat.boat.VX, boat.boat.VY, boat.boat.VZ = 0.2, -0.3, 0.4
	cart := s.newMinecart(nil, entity.MinecartPlain, uuid.New(), 3, 90, 3)
	cart.cart.VX, cart.cart.VY, cart.cart.VZ, cart.cart.Yaw = 0.2, -0.3, 0.4, 30
	stand := &armorStandEntity{stand: entity.NewArmorStand(3, 3, 90, 3)}
	stand.stand.VX, stand.stand.VY, stand.stand.VZ, stand.stand.Yaw = 0.2, -0.3, 0.4, 30
	rocket := &fireworkEntity{rocket: entity.NewFirework(4, 3, 90, 3, 0, 0, 0, 0, 1)}
	rocket.rocket.VX, rocket.rocket.VY, rocket.rocket.VZ, rocket.rocket.Yaw, rocket.rocket.Pitch = 0.2, -0.3, 0.4, 30, 10
	for _, tc := range []struct {
		name string
		move func()
		tag  func() nbt.Compound
	}{
		{"boat", func() { s.gatewayMoveBoat(boat, player.Position{X: 20.5, Y: 120, Z: 20.5}) }, func() nbt.Compound { return boatTag(boat).Entries() }},
		{"minecart", func() { s.gatewayMoveMinecart(cart, player.Position{X: 20.5, Y: 120, Z: 20.5}) }, func() nbt.Compound { return minecartTag(cart).Entries() }},
		{"armor stand", func() { s.gatewayMoveArmorStand(stand, player.Position{X: 20.5, Y: 120, Z: 20.5}) }, func() nbt.Compound { return armorStandTag(stand).Entries() }},
		{"firework", func() { s.gatewayMoveFirework(rocket, player.Position{X: 20.5, Y: 120, Z: 20.5}) }, func() nbt.Compound { return fireworkTag(rocket).Entries() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.move()
			tag := tc.tag()
			if x, y, z, ok := entityPositionOfTag(tag); !ok || x != 20.5 || y != 120 || z != 20.5 {
				t.Fatalf("landing = %v/%v/%v, ok=%v", x, y, z, ok)
			}
			if vx, vy, vz := entityMotionOfTag(tag); vx != 0.2 || vy != -0.3 || vz != 0.4 {
				t.Fatalf("motion changed to %v/%v/%v", vx, vy, vz)
			}
			if yaw, pitch := entityRotationOfTag(tag); yaw != 30 || tc.name == "firework" && pitch != 10 {
				t.Fatalf("rotation changed to %v/%v", yaw, pitch)
			}
		})
	}
}

func TestGatewayBoatCarriesIndirectMobPassengers(t *testing.T) {
	w := &world.World{}
	boat := &boatEntity{w: w, boat: entity.NewBoat(1, 3.5, 90, 3.5, 0)}
	mount := &mobEntity{w: w, vehicle: boat, mob: &entity.Mob{ID: 2, X: 3.5, Y: 90.5, Z: 3.5, VX: 0.2}}
	rider := &mobEntity{w: w, mob: &entity.Mob{ID: 3, X: 3.5, Y: 91, Z: 3.5, VY: -0.3}}
	mount.mob.Passengers = []*entity.Mob{rider.mob}
	rider.mob.Vehicle = mount.mob
	boat.passengers = []*boatPassenger{{ent: mount}}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: &mobHerd{book: map[*entity.Mob]*mobEntity{
		mount.mob: mount, rider.mob: rider,
	}}}})
	s.gatewayMoveBoat(boat, player.Position{X: 20.5, Y: 120, Z: 20.5})
	if mount.mob.X != 20.5 || mount.mob.Y != 120.5 || mount.mob.Z != 20.5 || mount.mob.VX != 0.2 || !mount.teleported {
		t.Fatalf("mount lost its offset, motion or teleport: %+v", mount.mob)
	}
	if rider.mob.X != 20.5 || rider.mob.Y != 121 || rider.mob.Z != 20.5 || rider.mob.VY != -0.3 || !rider.teleported {
		t.Fatalf("indirect rider lost its offset, motion or teleport: %+v", rider.mob)
	}
	if mount.vehicle != boat || rider.mob.Vehicle != mount.mob {
		t.Fatal("gateway dismounted a passenger")
	}
}
