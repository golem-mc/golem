package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A burning body in powder snow is put out, and the block melts for a player
// and for a mob only while mob_griefing is on.
// vanilla: PowderSnowBlock.entityInside

func TestPowderSnowPutsOutAndMeltsForABurningPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Smoulder")
	c.MoveToGround(0.5, 64, 90.5, true)
	c.Settle()

	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 90, servertest.BlockState(t, "minecraft:powder_snow")) })
	srv.SetPlayerFire("Smoulder", 200)
	tick(1)
	c.Settle()

	if fire := srv.PlayerFire("Smoulder"); fire > 0 {
		t.Errorf("the burning player kept %d fire ticks in powder snow, want it put out", fire)
	}
	if srv.BlockAt(0, 64, 90) != 0 {
		t.Error("the powder snow stayed where a burning player stood in it, want it melted")
	}
}

func TestPowderSnowLeavesACoolPlayerAlone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Cool")
	c.MoveToGround(0.5, 64, 92.5, true)
	c.Settle()

	snow := servertest.BlockState(t, "minecraft:powder_snow")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(0, 64, 92, snow) })
	tick(3)
	c.Settle()
	if srv.BlockAt(0, 64, 92) != snow {
		t.Error("the powder snow melted under a player who was not burning")
	}
}
