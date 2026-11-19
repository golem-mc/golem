package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// walkVibrations walks a mob a strip of floor toward a still player twelve
// blocks off, with a sculk sensor beside the strip, for 60 ticks. It returns
// the game events seen, whether the sensor left inactive, and the server and
// mob so a test can check the mob really walked.
func walkVibrations(t *testing.T, kind string, etype int32, floor string, ticks int) (*gameEvents, bool, *server.Server, int32) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "survival"
	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	c := connect()
	c.JoinAs("Prey")
	laid := servertest.BlockState(t, floor)
	for x := int32(-2); x <= 16; x++ {
		for z := int32(-2); z <= 2; z++ {
			srv.SetBlock(x, 63, z, laid)
		}
	}
	srv.SetBlock(6, 64, 3, servertest.BlockState(t, "minecraft:sculk_sensor"))
	c.MoveToGround(-0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	id := srv.SpawnMob(kind, 12.5, 64, 0.5)
	c.ExpectMobSpawn(etype)
	heard := false
	for range ticks {
		servertest.RunTicks(t, srv, 1)
		_, props := servertest.StatePropsOf(t, srv, 6, 64, 3)
		heard = heard || props["sculk_sensor_phase"] != "inactive"
	}
	return events, heard, srv, id
}

func stepsPastX(e *gameEvents, x float64) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, p := range e.seen["minecraft:step"] {
		if p[0] > x {
			n++
		}
	}
	return n
}

// TestAWardenWalksUnheard: a warden dampens vibrations, so its steps are
// posted as none.
// vanilla: Warden.dampensVibrations
func TestAWardenWalksUnheard(t *testing.T) {
	t.Parallel()
	// A spawned warden emerges for about 90 ticks before it walks.
	events, _, srv, id := walkVibrations(t, "minecraft:warden", protocol.EntityTypeWarden, "minecraft:stone", 200)
	if x, _, _, ok := srv.MobPos(id); !ok || x >= 8 {
		t.Fatalf("warden at x %v (present %v), want it walked below 8", x, ok)
	}
	if n := stepsPastX(events, 2); n != 0 {
		t.Errorf("%d step events at x > 2, want none", n)
	}
}
