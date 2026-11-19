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
