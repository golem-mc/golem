package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAZombieKeepsStrikingAPlayerWhoStopsAtABlockEdge has a zombie chase a
// player who steps a block and a half away and then stands still on the far
// edge of their block. The re-search the step provokes ends next to that
// block, out of the zombie's reach; the hunt must end with that spent path
// and start again with canUse's own search into the player's cell, not stand
// idle until the player moves again.
// vanilla: MeleeAttackGoal.canContinueToUse, MeleeAttackGoal.canUse
func TestAZombieKeepsStrikingAPlayerWhoStopsAtABlockEdge(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight keeps the daylight burn out of the hunt
	c := connect()
	c.JoinAs("Prey")
	pos := srv.PlayerPosition("Prey")

	// The zombie comes from the east; the player ends on the west edge of the
	// block two to the west of where it stood.
	srv.SpawnMob("minecraft:zombie", pos.X+4, pos.Y, pos.Z)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)
	edge := math.Floor(pos.X) - 2 + 0.03

	const (
		stepAt   = 25
		settleBy = 150
		total    = 300
	)
	blows := 0
	heal := func(counting bool) func(int32, []byte) bool {
		return func(id int32, data []byte) bool {
			if id != protocol.IDPlaySetHealth {
				return false
			}
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				if counting {
					blows++
				}
				// Keep the player alive so the hunt can go on.
				srv.SetHealth("Prey", 20)
			}
			return false
		}
	}
	c.PumpTicks(srv, stepAt, heal(false))
	c.MoveTo(edge, pos.Y, pos.Z, 0, 0)
	c.PumpTicks(srv, settleBy-stepAt, heal(false))
	c.PumpTicks(srv, total-settleBy, heal(true))
	if blows < 4 {
		t.Errorf("the zombie struck %d times in %d ticks against a player standing still, want at least 4", blows, total-settleBy)
	}
}
