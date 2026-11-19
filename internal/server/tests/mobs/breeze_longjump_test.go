package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABreezeLongJumpsAtATargetBeyondShootRange: a breeze 20 blocks from a
// survival player long-jumps at them instead of sliding into shoot range, and
// once its hurt by that player is over 40 ticks old, a landing out of shoot
// range waits the 10-tick jump cooldown before the next jump. The breeze is
// watched through its position: at 20 blocks its sounds are out of earshot.
// vanilla: Shoot.checkExtraStartConditions, LongJump.canRun, LongJump.tick
func TestABreezeLongJumpsAtATargetBeyondShootRange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Leapt")
	servertest.HoldTicks(t, srv)
	// Leapt joins at 0.5 64 0.5, on the flat test world's floor.
	id := srv.SpawnMob("minecraft:breeze", 20.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeBreeze)

	px, pz := 0.5, 0.5
	tick := 0
	// untilAirborne runs ticks until the breeze is airborne (or, with
	// airborne false, grounded again), and reports whether it was within n.
	untilAirborne := func(n int, airborne bool) bool {
		for range n {
			servertest.RunTicks(t, srv, 1)
			tick++
			if srv.MobOnGround(id) != airborne {
				return true
			}
		}
		return false
	}
	distance := func() float64 {
		x, _, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("the breeze is gone")
		}
		return math.Hypot(x-px, z-pz)
	}

	// landThenMoveTarget waits for the breeze's next landing, which arms its
	// shoot memory, moves its target 20 blocks out of shoot range, and runs
	// until the breeze is airborne again. It returns the ticks from landing
	// to takeoff and the breeze's distance from its target at takeoff.
	landThenMoveTarget := func() (int, float64) {
		t.Helper()
		if !untilAirborne(300, true) || !untilAirborne(100, false) {
			t.Fatal("the breeze made no long jump within 300 ticks")
		}
		landed := tick
		x, _, z, _ := srv.MobPos(id)
		px, pz = x+20, z
		srv.RunLine(fmt.Sprintf("tp Leapt %.2f 64 %.2f", px, pz))
		if !untilAirborne(300, true) {
			t.Fatal("the breeze never left the ground within 300 ticks of its target moving 20 blocks away")
		}
		return tick - landed, distance()
	}

	// One tick of flight covers about a block.
	if _, d := landThenMoveTarget(); d <= 16 {
		t.Fatalf("the breeze took off %.1f blocks from its target, want a long jump from beyond the 16 of shoot range rather than a slide closer", d)
	}

	srv.RunLine("damage @e[type=minecraft:breeze,limit=1] 1 minecraft:player_attack by Leapt")
	servertest.RunTicks(t, srv, 100)
	// 10 ticks of cooldown, then 10 of inhaling; a breeze hurt within 40
	// ticks would wait only 2.
	if gap, _ := landThenMoveTarget(); gap < 20 {
		t.Errorf("the breeze took off %d ticks after landing, want at least 20: a 10-tick cooldown and a 10-tick inhale", gap)
	}
}
