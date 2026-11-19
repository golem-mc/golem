package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestFoodTickTimerSurvivesARejoin checks that an empty bar's 80-tick
// starvation clock is saved and read back: 60 ticks into it, a rejoin takes
// the bite within the next 20 ticks rather than starting the clock over.
func TestFoodTickTimerSurvivesARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg)

	c := connect().Join("Hungry")
	servertest.HoldTicks(t, srv)
	// A full bar of health keeps the clock at 0 until the food empties.
	srv.SetHealth("Hungry", 20)
	srv.SetFood("Hungry", 0)
	servertest.RunTicks(t, srv, 60)
	if got := srv.PlayerHealth("Hungry"); got != 20 {
		t.Fatalf("health = %v before the rejoin, want 20 (no bite yet)", got)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Hungry"))
	if timer, ok := nbt.Get[nbt.Int](root, "foodTickTimer"); !ok || timer != 60 {
		t.Errorf("foodTickTimer = %v, %v; want 60", timer, ok)
	}

	// The rejoin runs with the loop still held, so no tick of it can bite
	// before Player Loaded, which would be refused and restart the clock. A
	// clock restarted at 0 would not bite for 80.
	connect().Join("Hungry")
	servertest.UntilTicks(t, srv, 20, "a starvation bite within 20 ticks of the rejoin", func() bool {
		return srv.PlayerHealth("Hungry") < 20
	})
}
