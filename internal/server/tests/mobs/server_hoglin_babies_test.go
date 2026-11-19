package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: HoglinAi.initIdleActivity, BabyFollowAdult.create
func TestABabyHoglinFollowsAnAdultHoglin(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	adult := srv.SpawnMobSeeded(hoglin, 24.5, 64, 0.5, 1)
	srv.SetMobNoAI(adult)
	baby := srv.SpawnMobSeeded(hoglin, 36.5, 64, 0.5, 2)
	srv.SetMobBaby(baby, true)
	servertest.RunTicks(t, srv, 200)
	ax, ay, az, _ := srv.MobPos(adult)
	bx, by, bz, ok := srv.MobPos(baby)
	if !ok {
		t.Fatal("the baby hoglin disappeared")
	}
	if d := math.Sqrt((bx-ax)*(bx-ax) + (by-ay)*(by-ay) + (bz-az)*(bz-az)); d > 6 {
		t.Errorf("the baby is %.2f blocks from the adult after 200 ticks, want at most 6", d)
	}
}

// vanilla: Piglin.startRiding, Piglin.getTopPassenger
func TestBabyPiglinsStackOnAHoglinUpToThreeRiders(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	hoglet := srv.SpawnMobSeeded(hoglin, 5.5, 64, 0.5, 1)
	srv.SetMobBaby(hoglet, true)
	srv.SetMobNoAI(hoglet)
	vehicle := hoglet
	for i := range 3 {
		piglet := srv.SpawnMobSeeded(barterPiglin, 5.5, 64, 0.5, int64(i+2))
		srv.SetMobBaby(piglet, true)
		srv.SetMobRideTarget(piglet, hoglet)
		data := c.ExpectWithinTicks(srv, 20, protocol.IDPlaySetPassengers)
		got, riders := servertest.ReadPassengers(c, data)
		if got != vehicle || len(riders) != 1 || riders[0] != piglet {
			t.Fatalf("rider %d: Set Passengers = %d carrying %v, want %d carrying [%d]", i+1, got, riders, vehicle, piglet)
		}
		if got := srv.MobVehicleID(piglet); got != vehicle {
			t.Fatalf("rider %d: vehicle = %d, want %d", i+1, got, vehicle)
		}
		vehicle = piglet
	}
	fourth := srv.SpawnMobSeeded(barterPiglin, 5.5, 64, 0.5, 5)
	srv.SetMobBaby(fourth, true)
	srv.SetMobRideTarget(fourth, hoglet)
	servertest.RunTicks(t, srv, 20)
	if got := srv.MobVehicleID(fourth); got != 0 {
		t.Errorf("a fourth piglin rides %d, want the full stack to refuse it", got)
	}
}
