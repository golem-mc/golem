package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// berryPushHealth pushes a mob of kind into a berry bush of the given age and
// returns its health afterwards.
func berryPushHealth(t *testing.T, kind string, age string) float64 {
	t.Helper()
	return berryHealth(t, kind, age, true)
}

// berryHealth is berryPushHealth, with the push optional: without it the mob
// starts inside the bush and stands still.
func berryHealth(t *testing.T, kind string, age string, push bool) float64 {
	t.Helper()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Picker")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(2, 64, 0, servertest.MustState(t, "minecraft:sweet_berry_bush", map[string]string{"age": age}))
	x := 3.5
	if !push {
		x = 2.5
	}
	id := srv.SpawnMob(kind, x, 64, 0.5)
	if push {
		srv.PushMob(id, -0.3, 0, 0)
	}
	servertest.RunTicks(t, srv, 10)
	hp, ok := srv.MobHealth(id)
	if !ok {
		t.Fatal("the mob is gone")
	}
	return hp
}

// vanilla: SweetBerryBushBlock.entityInside
func TestAPigPushedThroughARipeBerryBushIsPricked(t *testing.T) {
	t.Parallel()
	if hp := berryPushHealth(t, "minecraft:pig", "1"); hp >= 10 {
		t.Errorf("pig health = %v, want pricked", hp)
	}
}
