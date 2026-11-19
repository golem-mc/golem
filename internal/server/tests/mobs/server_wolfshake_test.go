package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// wolfYard joins one creative player and plants a wolf without AI at the
// middle of the cell (8, 64, 8) in the plains.
func wolfYard(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	c.Settle()
	// Held, so no tick runs between a test's block edit and its next count.
	servertest.HoldTicks(t, srv)
	srv.SetOverworldBiome(8, 8, "minecraft:plains")
	id := srv.SpawnMob("minecraft:wolf", 8.5, 64, 8.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("the wolf is not in the herd")
	}
	return srv, c, id
}

// wolfEvents steps n ticks and returns the entity event statuses the wolf
// drew in them, in order.
func wolfEvents(t *testing.T, srv *server.Server, c *servertest.Client, id int32, n uint64) []uint8 {
	t.Helper()
	servertest.AwaitTicks(t, srv, n)
	var got []uint8
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() == id {
			got = append(got, r.ReadU8())
		}
	})
	return got
}

func countStatus(got []uint8, status uint8) int {
	n := 0
	for _, g := range got {
		if g == status {
			n++
		}
	}
	return n
}

// TestAWolfShakesDryAfterLeavingTheWater: out of the water and on the
// ground it shakes once, and does not start another while it shakes.
// vanilla: Wolf.aiStep, Wolf.tick
func TestAWolfShakesDryAfterLeavingTheWater(t *testing.T) {
	t.Parallel()
	srv, c, id := wolfYard(t)
	srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	// Standing in the water it starts a shake each tick and is told to
	// stop it the same tick.
	if got := wolfEvents(t, srv, c, id, 5); countStatus(got, protocol.EntityStatusWolfStopShake) != countStatus(got, protocol.EntityStatusWolfShake) {
		t.Fatalf("a wolf in water drew events %v, want every shake stopped", got)
	}
	srv.SetBlock(8, 64, 8, 0)
	got := wolfEvents(t, srv, c, id, 10)
	if countStatus(got, protocol.EntityStatusWolfShake) != 1 || countStatus(got, protocol.EntityStatusWolfStopShake) != 0 {
		t.Errorf("a wolf out of the water drew events %v, want one shake", got)
	}
	if got := wolfEvents(t, srv, c, id, 60); len(got) != 0 {
		t.Errorf("a wolf that shook dry drew events %v, want none", got)
	}
}

// TestAWolfInTheRainStartsAndStopsItsShake: in the rain it wets, begins the
// shake and is told to stop it in the same tick; under a roof it stays dry.
// vanilla: Wolf.tick
func TestAWolfInTheRainStartsAndStopsItsShake(t *testing.T) {
	t.Parallel()
	for _, roofed := range []bool{false, true} {
		srv, c, id := wolfYard(t)
		if roofed {
			srv.SetBlock(8, 66, 8, servertest.MustState(t, "minecraft:stone", nil))
		}
		srv.SetWeatherState(true, false, 100000, 100000, 0)
		got := wolfEvents(t, srv, c, id, 30)
		if roofed && len(got) != 0 {
			t.Errorf("a wolf under a roof drew events %v, want none", got)
		}
		if !roofed && (countStatus(got, protocol.EntityStatusWolfShake) == 0 || countStatus(got, protocol.EntityStatusWolfStopShake) == 0) {
			t.Errorf("a wolf in the rain drew events %v, want a shake and its stop", got)
		}
	}
}
