package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// customStatOf reads one of a player's minecraft:custom statistics.
func customStatOf(srv *server.Server, name, key string) int32 {
	return srv.StatValue(name, registry.StatCustom, key)
}

// TestSneakingAndWakingTicksCountTheirTime: each tick of a held sneak adds one
// to sneak_time and one to time_since_rest, a tick off the key only the
// latter, and a death sets time_since_rest back to zero.
// vanilla: ServerPlayer.doTick, ServerPlayer.die
func TestSneakingAndWakingTicksCountTheirTime(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Sneaker")
	servertest.HoldTicks(t, srv)
	c.Settle()

	sneak := func() int32 { return customStatOf(srv, "Sneaker", "minecraft:sneak_time") }
	rest := func() int32 { return customStatOf(srv, "Sneaker", "minecraft:time_since_rest") }
	idle, awake := sneak(), rest()
	servertest.RunTicks(t, srv, 5)
	if got := sneak() - idle; got != 0 {
		t.Errorf("sneak_time grew by %d standing, want 0", got)
	}
	if got := rest() - awake; got != 5 {
		t.Errorf("time_since_rest grew by %d over five ticks, want 5", got)
	}

	c.SendSneak(true)
	c.Settle()
	idle = sneak()
	servertest.RunTicks(t, srv, 7)
	if got := sneak() - idle; got != 7 {
		t.Errorf("sneak_time grew by %d over seven sneaking ticks, want 7", got)
	}

	c.SendSneak(false)
	c.Settle()
	idle = sneak()
	servertest.RunTicks(t, srv, 3)
	if got := sneak() - idle; got != 0 {
		t.Errorf("sneak_time grew by %d after letting the key up, want 0", got)
	}

	if !srv.Hurt("Sneaker", 1000) {
		t.Fatal("the killing blow did not land")
	}
	if got := rest(); got != 0 {
		t.Errorf("time_since_rest = %d after a death, want 0", got)
	}
}

// TestADeathSetsTimeSinceDeathToZero: a death leaves a time_since_death
// objective's score at 0 rather than removing it.
// vanilla: ServerPlayer.resetStat, ScoreAccess.reset
func TestADeathSetsTimeSinceDeathToZero(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Mortal", servertest.OpsListing(t, "Mortal"))
	c.ChatCommand("scoreboard objectives add since minecraft.custom:minecraft.time_since_death")
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	if v, ok := srv.ScoreOf("Mortal", "since"); !ok || v <= 0 {
		t.Fatalf("since = %d, %v before the death, want a positive score", v, ok)
	}
	if !srv.Hurt("Mortal", 1000) {
		t.Fatal("the killing blow did not land")
	}
	if v, ok := srv.ScoreOf("Mortal", "since"); !ok || v != 0 {
		t.Errorf("since = %d, %v after the death, want 0, true", v, ok)
	}
}

// TestFallsOfTwoBlocksOrMoreCountFallDistance: a landing after a fall of five
// blocks adds 500 to fall_one_cm; a hop of one block adds nothing.
// vanilla: Player.causeFallDamage
func TestFallsOfTwoBlocksOrMoreCountFallDistance(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Faller")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	fell := func() int32 { return customStatOf(srv, "Faller", "minecraft:fall_one_cm") }

	c.MoveToGround(0.5, 65, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if got := fell(); got != 0 {
		t.Errorf("fall_one_cm = %d after a one-block drop, want 0", got)
	}

	c.MoveToGround(0.5, 69, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if got := fell(); got != 500 {
		t.Errorf("fall_one_cm = %d after a five-block drop, want 500", got)
	}
}

// TestAGameModeThatCannotFallCountsNoFallDistance: a creative player landing
// from five blocks adds nothing.
// vanilla: Player.causeFallDamage
func TestAGameModeThatCannotFallCountsNoFallDistance(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Flier")
	c.MoveToGround(0.5, 69, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if got := customStatOf(srv, "Flier", "minecraft:fall_one_cm"); got != 0 {
		t.Errorf("fall_one_cm = %d for a creative landing, want 0", got)
	}
}
