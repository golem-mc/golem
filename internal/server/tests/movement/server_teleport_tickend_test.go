package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAThrowAfterATeleportSizedMoveRidesNothing moves the test client far in
// one step and throws at once. A real client's next tick reports no move, so
// the thrower's known movement is zero; MoveTo sends the tick ends that
// clear it, so the egg leaves at the plain throw velocity.
// vanilla: ServerGamePacketListenerImpl.handleClientTickEnd
func TestAThrowAfterATeleportSizedMoveRidesNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:egg"), Count: 16})

	from := c.Pos()
	const x, z = 12.5, 12.5
	if math.Hypot(x-from.X, z-from.Z) <= servertest.TeleportSizedMove {
		t.Fatalf("the step from %v is not teleport-sized", from)
	}
	c.MoveTo(x, 64, z, -90, 0)
	servertest.UseHeld(c, -90, 0)
	egg := c.ExpectThrownSpawn(srv, protocol.EntityTypeEgg)

	if math.Abs(egg.Vx-1.5) > 0.06 || math.Abs(egg.Vz) > 0.06 {
		t.Errorf("the egg left at (vx %v, vz %v), want the throw's own power 1.5 east and nothing south", egg.Vx, egg.Vz)
	}
}

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
