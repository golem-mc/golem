package sleep

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
)

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
