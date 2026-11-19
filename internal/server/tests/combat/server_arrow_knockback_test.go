package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
)

// The shove an arrow's hit leaves on a player, watched from the victim's
// own wire: vanilla runs it along the shaft's horizontal flight rather than
// away from where the arrow struck, gates the default shove on a whole hurt,
// and lets a bow's Punch add its own push on top.

// victimShoves reads the shoves the server sends the body with the given
// entity ID while the next ticks run: the Set Entity Velocity packets whose
// first field is that body.
func victimShoves(c *servertest.Client, srv *server.Server, entityID int32, ticks int) [][3]float64 {
	var out [][3]float64
	c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		vx, vy, vz := r.ReadLpVec3()
		out = append(out, [3]float64{vx, vy, vz})
		return false
	})
	return out
}

// TestAnArrowsShoveFollowsItsFlight grazes a player's edge with two arrows
// fired west: the whole hit's default shove runs west along the flight, with
// none of the sideways share a push away from the arrow's position would
// carry, and the second arrow — faster, so it beats the window's last hurt
// and lands its excess alone — adds no default shove at all.
func TestAnArrowsShoveFollowsItsFlight(t *testing.T) {
	t.Parallel()
	srv, c, victimID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Both arrows fly west past the victim's near edge at z=0.75, so a shove
	// away from the arrow's position carries a sideways share. The first,
	// a four, opens the window; the second, a six, tops it and lands its
	// excess alone.
	srv.ShootArrow(4.5, 65, 0.75, -2, 0, 0)
	srv.ShootArrow(4.5, 65, 0.75, -3, 0, 0)

	shoves := victimShoves(c, srv, victimID, 40)
	if len(shoves) == 0 {
		t.Fatal("the victim was never shoved")
	}
	vx, _, vz := shoves[0][0], shoves[0][1], shoves[0][2]
	if vx >= 0 {
		t.Errorf("the shove's x = %v, want it west along the flight", vx)
	}
	if math.Abs(vz) > 0.05 {
		t.Errorf("the shove carried a sideways z = %v, want it along the flight alone", vz)
	}
	if len(shoves) != 1 {
		t.Errorf("the victim was shoved %d times, want the one whole hit's shove — "+
			"a hit softened by the window adds no default knockback", len(shoves))
	}
}

// TestAPunchBowAddsItsPushToTheShot checks the second shove a Punch bow
// leaves: on top of the whole hit's default, Punch II adds its 0.6 a level
// along the flight and a tenth upward, before travel applies drag and gravity.
func TestAPunchBowAddsItsPushToTheShot(t *testing.T) {
	t.Parallel()
	srv, c, victimID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)

	bow := servertest.EnchantedStack(t, "minecraft:bow", "minecraft:punch", 2)
	srv.ShootArrowFromWeapon(4.5, 65, 0.75, -2, 0, 0, bow)

	shoves := victimShoves(c, srv, victimID, 40)
	if len(shoves) == 0 {
		t.Fatal("the punch shot was never shoved")
	}
	vx, vy, vz := shoves[0][0], shoves[0][1], shoves[0][2]
	if want := (-0.4 - 1.2) * 0.546; math.Abs(vx-want) > 0.02 {
		t.Errorf("the punch shot shoved at x = %v, want the default and Punch II after ground drag (%v)", vx, want)
	}
	if want := (0.3608 + 0.1 - 0.08) * 0.98; math.Abs(vy-want) > 0.02 {
		t.Errorf("the punch shot lifted at y = %v, want the default and Punch lift after gravity (%v)", vy, want)
	}
	if math.Abs(vz) > 0.02 {
		t.Errorf("the punch shot carried a sideways z = %v, want it along the flight alone", vz)
	}
}
