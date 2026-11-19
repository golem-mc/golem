package sleep

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestLeavingBedUsesFourthOffsetBehindFoot(t *testing.T) {
	t.Parallel()
	testLeavingBedExit(t, false, true, 5.5, -0.5)
}

func testLeavingBedExit(t *testing.T, onlyDangerous, fourthOffset bool, wantX, wantZ float64) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)
	srv.StopTick()
	stone := servertest.MustState(t, "minecraft:stone", nil)
	magma := servertest.MustState(t, "minecraft:magma_block", nil)
	// South-facing bed, yaw 0: west side first, then behind the foot.
	surround := [][2]int32{{4, 1}, {4, 0}, {4, -1}, {5, -1}, {6, -1}, {6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2}}
	for _, p := range surround {
		if !srv.SetBlock(p[0], 63, p[1], stone) && srv.BlockAt(p[0], 63, p[1]) != stone {
			t.Fatal("could not lay exit floor")
		}
	}
	if !fourthOffset && !srv.SetBlock(4, 63, 1, magma) {
		t.Fatal("could not lay magma floor")
	}
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.SetPlayerGameMode("Actor", protocol.GameModeSurvival)
	// Block after entering bed so the sleep-start obstruction check passes.
	for i, p := range surround {
		if onlyDangerous && i > 0 || fourthOffset && i < 3 {
			if !srv.SetBlock(p[0], 64, p[1], stone) {
				t.Fatal("could not block exit")
			}
		}
	}
	if onlyDangerous {
		for _, z := range []int32{0, 1} {
			if !srv.SetBlock(5, 65, z, stone) {
				t.Fatal("could not block bed top")
			}
		}
	}
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.LeaveBed()
	data := c.ExpectWithinTicks(srv, 1, protocol.IDPlaySyncPosition)
	sync := servertest.ReadSyncPosition(t, data)
	if sync.X != wantX || sync.Y != 64 || sync.Z != wantZ {
		t.Errorf("wake position = (%g,%g,%g), want (%g,64,%g)", sync.X, sync.Y, sync.Z, wantX, wantZ)
	}
	c.MoveToGround(sync.X, sync.Y, sync.Z, true)
	c.Settle()
	if !onlyDangerous {
		c.ExpectNoneWithinTicks(srv, 20, servertest.PacketIs(protocol.IDPlayDamageEvent))
		if health := srv.PlayerHealth("Actor"); health != 20 {
			t.Errorf("health after 20 ticks = %g, want 20", health)
		}
	}
}
