package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// teamShot puts Shooter and Victim on team blue with the friendly fire given,
// shoots a player's arrow level at the victim's chest and reports whether it
// bounced off and whether it landed beyond them.
func teamShot(t *testing.T, friendlyFire bool) (srv *server.Server, bounced, landed bool) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), func(*server.Server) {})

	shooter := connect()
	shooter.JoinAs("Shooter")
	shooter.MoveToGround(5.5, 64, 0.5, true)
	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	shooter.Settle()
	victim.Settle()

	srv.RunLine("team add blue")
	if !friendlyFire {
		srv.RunLine("team modify blue friendlyFire false")
	}
	srv.RunLine("team join blue Shooter")
	srv.RunLine("team join blue Victim")

	arrowID := srv.ShootPlayerArrow("Shooter", 4.5, 65, 0.5, -2, 0, 0)
	shot := &deflectionWatch{c: shooter, arrowID: arrowID, bodyID: victimID}
	struck := &deflectionWatch{c: victim, arrowID: arrowID, bodyID: victimID}
	watchDeflection(t, srv, servertest.SkeletonTicks, func() bool {
		return shot.landed && struck.landed || shot.bounced || struck.bounced ||
			srv.PlayerHealth("Victim") < 20
	}, shot, struck)
	return srv, shot.bounced || struck.bounced, shot.landed && struck.landed
}

// TestAPlayersArrowPassesATeammateWithoutFriendlyFire shoots with pvp on at
// a teammate on a team with friendly fire off: the shaft is not deflected,
// flies on to land beyond them and draws no blood.
// vanilla: AbstractArrow.canHitEntity, Player.canHarmPlayer
func TestAPlayersArrowPassesATeammateWithoutFriendlyFire(t *testing.T) {
	t.Parallel()
	srv, bounced, landed := teamShot(t, false)
	if bounced {
		t.Error("the arrow was deflected off a teammate under friendly fire off")
	}
	if !landed {
		t.Error("the arrow never flew through the teammate to land beyond them")
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Errorf("teammate health after the arrow = %v, want 20", got)
	}
}

// TestAPlayersArrowHurtsATeammateWithFriendlyFire is the same shot with
// friendly fire on: the teammate is hurt.
func TestAPlayersArrowHurtsATeammateWithFriendlyFire(t *testing.T) {
	t.Parallel()
	srv, _, _ := teamShot(t, true)
	if got := srv.PlayerHealth("Victim"); got >= 20 {
		t.Errorf("teammate health after the arrow = %v, want a hurt", got)
	}
}

// TestASpearJabSparesATeammateWithoutFriendlyFire jabs a teammate with pvp
// on: spared while the team's friendly fire is off, hurt once it is on.
// vanilla: PiercingWeapon.canHitEntity, Player.canHarmPlayer
func TestASpearJabSparesATeammateWithoutFriendlyFire(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	jabber(t, srv, c, "Jabber")
	v := connect()
	v.JoinAs("Mate")
	v.MoveToGround(5.5, 64, 8.5, true)
	v.MoveToGround(5.5, 64, 8.5, true)
	v.Settle()
	c.Settle()
	srv.RunLine("team add blue")
	srv.RunLine("team modify blue friendlyFire false")
	srv.RunLine("team join blue Jabber")
	srv.RunLine("team join blue Mate")

	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := srv.PlayerHealth("Mate"); math.Abs(got-20) > 0.01 {
		t.Errorf("teammate health after a jab under friendly fire off = %v, want 20", got)
	}
	if got := srv.InventorySlot("Jabber", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("a jab at a spared teammate wore the spear to %d, want 0", got)
	}

	srv.RunLine("team modify blue friendlyFire true")
	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := srv.PlayerHealth("Mate"); got >= 20 {
		t.Errorf("teammate health after a jab under friendly fire on = %v, want a hurt", got)
	}
}
