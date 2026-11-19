package combat

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A melee blow on a player folds into the victim's kept velocity: each
// knockback halves the old horizontal motion, and an airborne victim keeps
// its vertical motion rather than being lifted.
// vanilla: LivingEntity.knockback, Player.causeExtraKnockback

// strikeMovingVictim joins a fencer on the ground at (2.5, 64, 5.5) facing
// east and a victim at (4.5, 64.5 or 64, 5.5), gives the victim the kept
// velocity v, lets the fencer's full-strength blow with held land on it and
// returns the shove the victim's client is sent.
func strikeMovingVictim(t *testing.T, held protocol.ItemStack, onGround bool, v [3]float64, prep ...func(fencer *servertest.Client)) []float64 {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Fencer"))
	victim := connect()
	victimID := victim.JoinAs("Victim")
	y := 64.0
	if !onGround {
		y = 64.5
	}
	victim.MoveToGround(4.5, y, 5.5, onGround)
	victim.Settle()
	fencer := connect().Join("Fencer")
	fencer.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	fencer.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	fencer.Settle()
	for _, p := range prep {
		p(fencer)
	}
	if held.Count > 0 {
		srv.GiveSlot("Fencer", player.HotbarSlot, held)
	}
	servertest.AwaitFullSwing(t, srv, "Fencer")

	servertest.HoldTicks(t, srv)
	victim.Settle()
	srv.SetPlayerVelocity("Victim", v[0], v[1], v[2])
	fencer.Attack(victimID)
	// The blow is taken on the fencer's connection, so it is waited on
	// without running a tick that would age the kept velocity.
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.AttackTicks("Fencer") < servertest.SwingRecovered
	})
	var shove []float64
	victim.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySetEntityMotion {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == victimID {
			x, y, z := r.ReadLpVec3()
			shove = []float64{x, y, z}
		}
	})
	return shove
}

// TestAnAirborneVictimKeepsItsVerticalVelocity: a bare-handed blow from the
// west on a falling victim moving (0.3, -0.2, 0.1) halves the horizontal
// and adds the 0.4 east, leaving the fall alone.
func TestAnAirborneVictimKeepsItsVerticalVelocity(t *testing.T) {
	t.Parallel()
	shove := strikeMovingVictim(t, protocol.ItemStack{}, false, [3]float64{0.3, -0.2, 0.1})
	wantShove(t, shove, [3]float64{0.15 + 0.4, -0.2, 0.05}, "the airborne victim")
}

// TestAGroundedVictimsVelocityIsHalvedByEachKnockback: a Knockback I blow
// from the west, the fencer facing east, on a grounded victim moving 0.4
// east. The hurt's knockback leaves 0.2+0.4, the extra halves that and adds
// its 0.5; the lift stays at the 0.4 cap.
func TestAGroundedVictimsVelocityIsHalvedByEachKnockback(t *testing.T) {
	t.Parallel()
	sword := servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 1)
	shove := strikeMovingVictim(t, sword, true, [3]float64{0.4, 0, 0})
	wantShove(t, shove, [3]float64{(0.2+0.4)/2 + 0.5, 0.4, 0}, "the grounded victim")
}
