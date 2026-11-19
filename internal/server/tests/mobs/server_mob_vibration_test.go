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

// TestAWalkingMobPostsStepGameEvents: a creeper's steps are vibrations on stone
// that a sculk sensor hears, and none on wool, which dampens them.
// vanilla: Entity.vibrationAndSoundEffectsFromBlock
func TestAWalkingMobPostsStepGameEvents(t *testing.T) {
	t.Parallel()
	for floor, wantHeard := range map[string]bool{"minecraft:stone": true, "minecraft:white_wool": false} {
		t.Run(floor, func(t *testing.T) {
			t.Parallel()
			events, heard, _, _ := walkVibrations(t, "minecraft:creeper", protocol.EntityTypeCreeper, floor, 60)
			if n := stepsPastX(events, 2); n < 3 {
				t.Errorf("%d step events at x > 2, want at least 3", n)
			}
			if heard != wantHeard {
				t.Errorf("sensor heard = %v, want %v", heard, wantHeard)
			}
		})
	}
}

// TestALandingMobPostsHitGround: a cow dropped two blocks posts hit_ground at
// its landing.
// vanilla: Entity.checkFallDamage
func TestALandingMobPostsHitGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	srv.SpawnMob("minecraft:cow", 2.5, 66, 0.5)
	servertest.RunTicks(t, srv, 20)
	hits := events.seen["minecraft:hit_ground"]
	if len(hits) == 0 {
		t.Fatal("no hit_ground event")
	}
	if h := hits[0]; h[1] < 63.99 || h[1] > 64.01 || h[0] < 2.49 || h[0] > 2.51 || h[2] < 0.49 || h[2] > 0.51 {
		t.Errorf("first hit_ground at %v, want 2.5, 64, 0.5", h)
	}
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
