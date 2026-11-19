package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// The pvp game rule: while it is off, a player's punch, sword sweep, arrow
// and player-lit blast leave another player's health whole — the arrow
// flying through the victim rather than being deflected, a blast still
// shoving the body — and the attacker still hears the no-damage sound. A
// mob's blow, a fall and fire are nobody's player hurt and keep landing.

// TestPvpOffTurnsAPlayersPunchAndSweepAside runs a two-player fight with
// pvp off: a bare-handed punch is refused with the no-damage sound and no
// health lost, and a full sword's sweep at a cow still leaves the player
// standing in the sweep's reach whole while the mob beside them takes the
// sweep's share.
func TestPvpOffTurnsAPlayersPunchAndSweepAside(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.PvpOffServer(t)

	fencer := connect()
	fencer.JoinAs("Fencer")
	fencer.MoveToGround(2.5, 64, 5.5, true)
	fencer.MoveToGround(2.5, 64, 5.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(4.5, 64, 5.5, true)
	victim.MoveToGround(4.5, 64, 5.5, true)
	fencer.Settle()
	victim.Settle()

	// The punch: refused outright, the attacker told by the no-damage
	// sound alone.
	fencer.Attack(victimID)
	if h := fencer.HearBlow(victimID); !slices.Equal(h.Sounds, []string{"nodamage"}) {
		t.Errorf("attack sounds of the refused punch = %v, want nodamage alone", h.Sounds)
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Errorf("health after the refused punch = %v of twenty, want no hurt", got)
	}

	// The sweep: a full sword at a cow with the victim one block south of
	// it. The mob beside the cow takes the sweep's plain one; the victim,
	// also in the sweep's reach, takes nothing.
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	srv.StopTick()
	victim.MoveToGround(4.5, 64, 4.5, true)
	victim.Settle()
	stricken := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	beside := srv.SpawnMob("minecraft:cow", 5.45, 64, 5.5)
	fencer.Settle()
	victim.Settle()

	fencer.Attack(stricken)
	fencer.Settle()
	heard := victim.HearBodies()
	if got := heard.Healths[beside]; math.Abs(float64(got)-9) > 0.01 {
		t.Errorf("the mob swept beside the victim was left at %v, want the sweep's one off a ten", got)
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Errorf("health after the sweep = %v of twenty, want no hurt", got)
	}
}

// TestPvpOffAPlayersArrowPassesThroughAPlayer shoots a shaft a player owns
// at another player with pvp off: vanilla's canHitEntity skips the victim
// before the sweep, so the shaft flies through with no deflection and lands
// beyond them, the victim's health and fire both untouched.
func TestPvpOffAPlayersArrowPassesThroughAPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.PvpOffServer(t)

	shooter := connect()
	shooter.JoinAs("Shooter")
	shooter.MoveToGround(5.5, 64, 0.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	shooter.Settle()
	victim.Settle()

	// The arrow starts between the shooter and the victim and flies level
	// at the victim's chest; a burning shaft would leave the body alight if
	// the hit were taken at all.
	arrowID := srv.ShootPlayerArrow("Shooter", 4.5, 65, 0.5, -2, 0, 0)
	srv.IgniteArrow(arrowID, 100)
	shot := &deflectionWatch{c: shooter, arrowID: arrowID, bodyID: victimID}
	struck := &deflectionWatch{c: victim, arrowID: arrowID, bodyID: victimID}
	watchDeflection(t, srv, servertest.SkeletonTicks, func() bool {
		return shot.landed && struck.landed
	}, shot, struck)

	if shot.bounced || struck.bounced {
		t.Error("the player's arrow was deflected off the victim under pvp off")
	}
	if !shot.landed || !struck.landed {
		t.Fatal("the player's arrow never flew through the victim to land beyond them")
	}
	if shot.hurt || struck.hurt {
		t.Error("the player's arrow drew blood under pvp off")
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Errorf("victim health after the arrow = %v of twenty, want no hurt", got)
	}
	if got := srv.PlayerFire("Victim"); got > 0 {
		t.Errorf("victim fire after the burning arrow = %d ticks, want no burn", got)
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the arrow flown through the victim is not in the world any more")
	}
}

// TestPvpOffLeavesAMobArrowHittingAPlayer shoots a shaft a mob owns at a
// player with pvp off: SparePlayers is the player-owned rule alone, so the
// mob's arrow still draws blood.
func TestPvpOffLeavesAMobArrowHittingAPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.PvpOffServer(t)

	victim := connect()
	victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	victim.Settle()

	// Spawn the cow only once the victim holds the chunk: a mob dropped in an
	// unheld chunk is unloaded and re-raised with a new ID before the shot.
	shooter := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)

	arrowID := srv.ShootMobArrow(shooter, 4.5, 65, 0.5, -2, 0, 0)
	if arrowID == 0 {
		t.Fatal("the mob's arrow was not stood in the world")
	}
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerHealth("Victim") < 20 })
	if got := srv.PlayerHealth("Victim"); got >= 20 {
		t.Errorf("victim health after the mob's arrow = %v of twenty, want a hurt", got)
	}
}

// TestPvpOffLeavesAPlayerLitTNTsVictimWholeButPushed lights a TNT with one
// player's hand and sets it off beside another with pvp off: the blast's
// cause is the lighter, so the hurt is refused and the victim's health
// stands at twenty, while the blast still shoves the body.
func TestPvpOffLeavesAPlayerLitTNTsVictimWholeButPushed(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.PvpOffServer(t)

	lighter := connect()
	lighter.JoinAs("Lighter")
	lighter.MoveToGround(0.5, 64, 0.5, true)

	victim := connect()
	victim.JoinAs("Victim")
	victim.MoveToGround(3.5, 64, 0.5, true)
	lighter.Settle()
	victim.Settle()

	srv.SpawnPlayerTNT("Lighter", 2.5, 64, 0.5, 4)

	pushed, hurt := false, false
	for id, data := range victim.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayExplosion:
			pushed = servertest.ReadExplosion(victim, data).Pushed
		case protocol.IDPlaySetHealth:
			if servertest.ExpectHealthReport(victim, data).Health < 20 {
				hurt = true
			}
		}
		if pushed {
			break
		}
	}
	if hurt {
		t.Error("the player-lit TNT drew blood under pvp off")
	}
	if !pushed {
		t.Fatal("the player-lit TNT never pushed the victim")
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Errorf("victim health after the player-lit TNT = %v of twenty, want no hurt", got)
	}
}

// TestPvpOffStillTakesMobFallAndFire turns pvp off and holds that the hurts
// no player is behind still land: a mob's blow, a fall and fire.
func TestPvpOffStillTakesMobFallAndFire(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim", func(s *server.Server) {
		s.SetGameRule("pvp", false)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// A mob's blow is not a player's, so the rule does not touch it.
	if !srv.Hurt("Victim", 1) {
		t.Error("a mob's blow was refused under pvp off")
	}
	if got := srv.PlayerHealth("Victim"); math.Abs(got-19) > 0.01 {
		t.Errorf("health after the mob's blow = %v, want 19", got)
	}
	srv.SetHealth("Victim", 20)

	// The fall: a body dropping six blocks still takes the last three.
	srv.StopTick()
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 67, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerHealth("Victim") < 20 })
	if got := srv.PlayerHealth("Victim"); math.Abs(got-17) > 0.01 {
		t.Errorf("health after the fall = %v, want 17", got)
	}
	srv.SetHealth("Victim", 20)

	// The fire: a body alight still burns.
	srv.SetPlayerFire("Victim", 20)
	srv.TickSurvival()
	if got := srv.PlayerHealth("Victim"); got >= 20 {
		t.Errorf("health after the fire = %v, want under 20", got)
	}
}
