package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLandingPastAnEdgeStillRestsOnTheBlockItLeft: a fall that ends with a
// sideways step whose box has just cleared a hay bale's edge, over air,
// still lands on the bale (damage multiplier 0.2), not as a fall onto
// nothing.
// vanilla: Entity.checkSupportingBlock
func TestLandingPastAnEdgeStillRestsOnTheBlockItLeft(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Edger")
	srv.SetBlock(5, 63, 0, servertest.MustState(t, "minecraft:hay_block", nil))
	srv.SetBlock(6, 63, 0, 0)
	c.MoveToGround(5.9, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.MoveToGround(5.9, 74, 0.5, false)
	c.MoveToGround(5.9, 65, 0.5, false)
	// The box now spans x 6.1..6.7: clear of the bale at 5..6.
	c.MoveToGround(6.4, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	// Ten blocks: 7 damage on stone, 1 on hay (multiplier 0.2).
	if got := 20 - srv.PlayerHealth("Edger"); math.Abs(float64(got)-1) > 1e-4 {
		t.Errorf("damage = %g, want 1 (landed on hay)", got)
	}
}
