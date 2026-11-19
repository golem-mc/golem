package mobs

import (
	"math"
	"testing"

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
