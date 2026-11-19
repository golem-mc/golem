package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"
)

// reinforcedZombie is one zombie standing after a call: where it is, whom
// it hunts, and whether it is the one the player struck.
type reinforcedZombie struct {
	id      int32
	x, y, z float64
	target  int32
	struck  bool
}

// reinforcementRun hits a zombie that is certain to call, in a world of the
// difficulty given, and returns the zombies standing after the ticks given
// together with the player's ID and the struck zombie's ID.
func reinforcementRun(t *testing.T, difficulty uint8, ticks uint64, rules ...string) (zombies []reinforcedZombie, playerID int32) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg)
	srv.SetSpawning(false)
	for _, rule := range rules {
		srv.SetGameRule(rule, false)
	}
	srv.SetDayTime(18000) // midnight: dark enough for the called zombie to stand
	srv.SetDifficulty(difficulty)
	c := connect()
	playerID = c.JoinAs("Attacker")

	servertest.HoldTicks(t, srv)
	struck := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
	srv.SetMobReinforcementBase(struck, 1)
	swing := func() {
		servertest.UntilTicks(t, srv, 40, "the swing recovers", func() bool { return srv.AttackTicks("Attacker") >= servertest.SwingRecovered })
	}
	swing()
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Attack(struck)
	c.Settle()

	// A call's site search is a handful of dice-driven tries that may all
	// miss, so a certain caller is struck again until one finds a site. The
	// loop is held so the zombies are read on the tick the call lands.
	for ran := uint64(0); ran < ticks && srv.MobCount() < 2; ran++ {
		servertest.RunTicks(t, srv, 1)
		if ran%20 == 19 && srv.MobCount() < 2 {
			swing()
			c.Attack(struck)
			c.Settle()
		}
	}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:zombie" {
			continue
		}
		zombies = append(zombies, reinforcedZombie{m.ID, m.X, m.Y, m.Z, srv.MobTargetID(m.ID), m.ID == struck})
	}
	return zombies, playerID
}

// TestAHurtZombieOnHardCallsAReinforcement checks Zombie.hurtServer: a zombie
// a player strikes on Hard, certain to call, brings a second zombie at least
// seven blocks off along some axis that hunts the same player.
func TestAHurtZombieOnHardCallsAReinforcement(t *testing.T) {
	t.Parallel()
	zombies, playerID := reinforcementRun(t, 3, 100)
	if len(zombies) != 2 {
		t.Fatalf("%d zombies stood after the struck one's call, want 2", len(zombies))
	}
	struck, called := zombies[0], zombies[1]
	if !struck.struck {
		struck, called = called, struck
	}
	if called.target != playerID {
		t.Errorf("the called zombie hunts %d, want the player %d", called.target, playerID)
	}
	// The site is 7 or more from the caller's block, not from its exact
	// position, so a caller standing late in its block can be under 7
	// blocks away by float.
	far := math.Max(math.Abs(math.Floor(called.x)-math.Floor(struck.x)),
		math.Max(math.Abs(math.Floor(called.y)-math.Floor(struck.y)), math.Abs(math.Floor(called.z)-math.Floor(struck.z))))
	if far < 7 {
		t.Errorf("the called zombie stands %.0f blocks from the caller's block on its farthest axis, want 7 or more", far)
	}
}

// TestAHurtZombieBelowHardCallsNoReinforcement checks the difficulty gate:
// Easy and Normal never call, whatever the chance.
func TestAHurtZombieBelowHardCallsNoReinforcement(t *testing.T) {
	t.Parallel()
	for _, difficulty := range []uint8{1, 2} {
		zombies, _ := reinforcementRun(t, difficulty, 100)
		if len(zombies) != 1 {
			t.Errorf("difficulty %d: %d zombies stood after a hurt, want the struck one alone", difficulty, len(zombies))
		}
	}
}
