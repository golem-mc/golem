package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWalkingStepSendsNoTickEnd keeps the short step's ride: the tick ends
// MoveTo sends for a teleport-sized move are not sent after a walk, which a
// throw must still ride.
func TestAWalkingStepSendsNoTickEnd(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Walker")
	srv.GiveSlot("Walker", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	from := c.Pos()
	c.MoveTo(from.X+0.2158, from.Y, from.Z, -90, 0)
	servertest.UseHeld(c, -90, 0)
	egg := c.ExpectThrownSpawn(srv, protocol.EntityTypeEgg)

	if got := egg.Vx - 1.5; math.Abs(got-0.2158) > 0.06 {
		t.Errorf("the egg left %v faster than the plain throw, want the step's 0.2158", got)
	}
}
