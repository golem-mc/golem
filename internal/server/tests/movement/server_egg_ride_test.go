package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAThrownEggRidesItsThrowersWalk throws two eggs level east from the
// same spot: one stood still, one released right after a walking-speed step
// east. Vanilla Projectile.shootFromRotation adds the thrower's
// getKnownMovement to the throw, so the moving egg leaves by the step the
// faster and the still one leaves at the plain shoot velocity the rotation
// and power give. The vertical ride is left out with the feet on the ground.
// vanilla: Projectile.shootFromRotation
func TestAThrownEggRidesItsThrowersWalk(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	// A walking step is 0.2158 blocks a tick. The same spot twice leaves the
	// standing throw nothing to ride. An egg's aim carries noise of a
	// hundredth or two either way, so each comparison leaves that much room.
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.UseHeld(c, -90, 0)
	standing := c.ExpectThrownSpawn(srv, protocol.EntityTypeEgg)

	c.MoveToGrounded(0.5+0.2158, 64, 0.5, -90, 0, true)
	servertest.UseHeld(c, -90, 0)
	walking := c.ExpectThrownSpawn(srv, protocol.EntityTypeEgg)

	if math.Abs(standing.Vx-1.5) > 0.06 {
		t.Errorf("the standing egg left at vx %v, want the throw's own power 1.5", standing.Vx)
	}
	if got := walking.Vx - standing.Vx; math.Abs(got-0.2158) > 0.06 {
		t.Errorf("the walking egg left %v faster than the standing one, want the step's 0.2158", got)
	}
	if got := walking.Vy; math.Abs(got) > 0.06 {
		t.Errorf("a grounded walk rode the egg's vertical %v, want the vertical part left out", got)
	}
}
