package sleep

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestRainDoesNotDouseABurningPlayerUnderAGlassRoof checks the MOTION_BLOCKING
// heightmap the rain reads: glass lets the sky light through at full strength
// but stops the rain, so a body burning under it stays alight where the open
// sky would put it out.
func TestRainDoesNotDouseABurningPlayerUnderAGlassRoof(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper")
	srv.StopTick()

	pos := srv.PlayerPosition("Camper")
	x, y, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	srv.SetBlock(x, y+2, z, registry.MustState("minecraft:glass", nil))
	srv.RelightAt(x, z)

	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 20 {
		srv.TickWeather()
	}
	if !srv.Raining() {
		t.Fatal("the world is not raining")
	}

	srv.SetPlayerFire("Camper", 100)
	srv.TickSurvival()
	if fire := srv.PlayerFire("Camper"); fire <= 0 {
		t.Errorf("a burning body under a glass roof = fire %d, want it alight", fire)
	}
}

// TestRainDousesABurningPlayerWhoseHeadAloneIsInTheOpen checks the top of the
// body's box in the rain read: a player standing on a bottom slab has their
// feet cell inside the slab, but their head in the open sky, which still puts
// the burn out. vanilla: Entity.isInRain
func TestRainDousesABurningPlayerWhoseHeadAloneIsInTheOpen(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Slabber")
	srv.StopTick()

	pos := srv.PlayerPosition("Slabber")
	x, y, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	// A bottom slab in the feet cell: the body stands with its feet at the
	// slab's top, so the rain reaches the head's cell alone.
	srv.SetBlock(x, y, z, registry.MustState("minecraft:stone_slab", nil))
	srv.RelightAt(x, z)
	c.MoveToGround(pos.X, float64(y)+0.5, pos.Z, true)
	c.Settle()

	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 20 {
		srv.TickWeather()
	}
	if !srv.Raining() {
		t.Fatal("the world is not raining")
	}

	srv.SetPlayerFire("Slabber", 100)
	srv.TickSurvival()
	if fire := srv.PlayerFire("Slabber"); fire > 0 {
		t.Errorf("a burning body whose head alone is in the rain = fire %d, want it put out", fire)
	}
}

// TestRainDousesABurningPlayerInTheOpen is the control for the roof test: the
// same rain puts the same fire out where the sky falls on the body.
func TestRainDousesABurningPlayerInTheOpen(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper")
	srv.StopTick()

	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 20 {
		srv.TickWeather()
	}
	if !srv.Raining() {
		t.Fatal("the world is not raining")
	}

	srv.SetPlayerFire("Camper", 100)
	srv.TickSurvival()
	if fire := srv.PlayerFire("Camper"); fire > 0 {
		t.Errorf("a burning body in the open rain = fire %d, want it put out", fire)
	}
}
