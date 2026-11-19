package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A player's knockback is kept server-side as the delta movement a ranged mob
// leads its aim by, not only sent to their client.

func TestAnArrowsShoveIsKeptAsThePlayersVelocity(t *testing.T) {
	t.Parallel()
	srv, c, victimID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)

	if x, y, z := srv.PlayerVelocity("Victim"); x != 0 || math.Abs(y+0.0784) > 1e-6 || z != 0 {
		t.Fatalf("velocity before the hit = (%v, %v, %v), want only resting gravity", x, y, z)
	}
	srv.ShootArrow(4.5, 65, 0.75, -2, 0, 0)

	shoves := firstShove(t, c, srv, victimID)
	// The mob tick may already have aged the shove once by the time it is read.
	x, _, z := srv.PlayerVelocity("Victim")
	want := shoves[0]
	if x >= 0 {
		t.Errorf("kept velocity x = %v, want it west along the flight", x)
	}
	if math.Abs(x) > math.Abs(want[0])+0.01 || math.Abs(z) > math.Abs(want[2])+0.01 {
		t.Errorf("kept velocity (%v, %v) exceeds the sent shove (%v, %v)", x, z, want[0], want[2])
	}
}

// A kept shove ages like a body's motion and is gone a few ticks later.
func TestAPlayersKeptVelocityDecays(t *testing.T) {
	t.Parallel()
	srv, c, victimID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.ShootArrow(4.5, 65, 0.75, -2, 0, 0)
	firstShove(t, c, srv, victimID)
	servertest.RunTicks(t, srv, 20)
	if x, y, z := srv.PlayerVelocity("Victim"); x != 0 || math.Abs(y+0.0784) > 1e-6 || z != 0 {
		t.Errorf("velocity 20 ticks after the hit = (%v, %v, %v), want only resting gravity", x, y, z)
	}
}

// firstShove runs ticks until the victim's first Set Entity Velocity arrives
// and returns it, so the kept velocity is read before it has aged far.
func firstShove(t *testing.T, c *servertest.Client, srv *server.Server, entityID int32) [][3]float64 {
	t.Helper()
	var out [][3]float64
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		vx, vy, vz := r.ReadLpVec3()
		out = append(out, [3]float64{vx, vy, vz})
		return true
	})
	if len(out) == 0 {
		t.Fatal("the victim was never shoved")
	}
	return out
}
