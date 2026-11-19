package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestSummonedBreezeShootsAWindChargeAtASurvivalPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:breeze 10.5 64 0.5")
	// The 10-block breeze long-jumps first; landing arms a 100-tick shot.
	if !servertest.ASpawnOfWithinTicks(c, srv, 200, protocol.EntityTypeBreezeWindCharge) {
		t.Fatal("summoned breeze did not shoot within 200 ticks")
	}
}
