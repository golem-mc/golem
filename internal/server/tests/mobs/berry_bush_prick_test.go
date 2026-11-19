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

func TestAPigPushedThroughAYoungBerryBushIsNotPricked(t *testing.T) {
	t.Parallel()
	if hp := berryPushHealth(t, "minecraft:pig", "0"); hp != 10 {
		t.Errorf("pig health = %v, want 10", hp)
	}
}

func TestAFoxPushedThroughARipeBerryBushIsNotPricked(t *testing.T) {
	t.Parallel()
	if hp := berryPushHealth(t, "minecraft:fox", "1"); hp != 10 {
		t.Errorf("fox health = %v, want 10", hp)
	}
}

// vanilla: SweetBerryBushBlock.entityInside (movement.horizontalDistanceSqr() >= 0.003)
func TestAStationaryPigInARipeBerryBushIsNotPricked(t *testing.T) {
	t.Parallel()
	if hp := berryHealth(t, "minecraft:pig", "1", false); hp != 10 {
		t.Errorf("pig health = %v, want 10", hp)
	}
}

// A pushed pig is pricked once: the bush's stuck multiplier spends its
// velocity on the next move, so only the first tick's step is >= 0.003.
// vanilla: SweetBerryBushBlock.entityInside, Entity.move
func TestAPigPushedIntoARipeBerryBushIsPrickedOnce(t *testing.T) {
	t.Parallel()
	if hp := berryPushHealth(t, "minecraft:pig", "3"); hp != 9 {
		t.Errorf("pig health = %v, want 9", hp)
	}
}
