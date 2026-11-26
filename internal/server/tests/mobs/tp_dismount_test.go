package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.teleportSameDimension stops the riding; a carried rider
// moves with its mount instead.
func TestTeleportDismountsOnlyADirectlyTargetedRider(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.RunLine(`summon chicken 2.5 64 0.5 {NoAI:1b,Tags:[mount],Passengers:[{id:"minecraft:pig",NoAI:1b,Tags:[rider]}]}`)
	chicken := srv.MobIDsOfKind("minecraft:chicken")[0]
	pig := srv.MobIDsOfKind("minecraft:pig")[0]
	if !slices.Equal(srv.MobPassengerIDs(chicken), []int32{pig}) {
		t.Fatalf("passengers = %v, want the pig", srv.MobPassengerIDs(chicken))
	}
	srv.RunLine(`tp @e[tag=mount] 10.5 64 10.5`)
	if got := srv.MobVehicleID(pig); got != chicken {
		t.Fatalf("pig vehicle after teleporting the mount = %d, want %d", got, chicken)
	}
	srv.RunLine(`tp @e[tag=rider] 20.5 64 20.5`)
	if got := srv.MobVehicleID(pig); got == chicken {
		t.Errorf("pig still rides the chicken")
	}
	if got := srv.MobPassengerIDs(chicken); len(got) != 0 {
		t.Errorf("chicken passengers = %v, want none", got)
	}
	x, y, z, _ := srv.MobPos(pig)
	if x != 20.5 || y != 64 || z != 20.5 {
		t.Errorf("pig at %v %v %v, want 20.5 64 20.5", x, y, z)
	}
}
