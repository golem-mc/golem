package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// What a shield-blocked hit does to the holder's movement, against the same
// hit unblocked. vanilla: LivingEntity.hurtServer, applyItemBlocking

// TestAnUnblockedZombieBlowShovesTheVictimAndTellsItsClient is the unshielded
// side: the hurt's knockback away from the zombie reaches the victim as
// Set Entity Motion after travel ages it.
// vanilla: LivingEntity.dealDefaultKnockback, ServerEntity.sendChanges
func TestAnUnblockedZombieBlowShovesTheVictimAndTellsItsClient(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := connect().Join("Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)

	var speed, vz float64
	var motion, damage bool
	c.ReadTicksSkipping(srv, shieldDeadlineTicks, shieldSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				vx, _, z := r.ReadLpVec3()
				speed, vz, motion = math.Hypot(vx, z), z, true
			}
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID {
				damage = true
			}
		}
		return motion && damage
	})
	if !motion || !damage {
		t.Fatalf("the unblocked zombie blow told motion %v and damage %v, want both", motion, damage)
	}
	if math.Abs(speed-0.2184) > 0.003 {
		t.Errorf("the unblocked blow shoved the victim at %v, want 0.2184 after ground drag", speed)
	}
	if vz >= 0 {
		t.Errorf("the unblocked blow shoved the victim at vz %v, want it away from the zombie", vz)
	}
}

// TestABlockedBabyHoglinBlowGivesTheHolderNoShieldShove: Hoglin overrides
// blockedByItem with no base shove (an adult throws its target instead), and
// a fully blocked blow deals no default knockback, so a baby's blocked blow
// leaves the holder's server-side movement still.
// vanilla: Hoglin.blockedByItem, LivingEntity.hurtServer
func TestABlockedBabyHoglinBlowGivesTheHolderNoShieldShove(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := shieldBlocker(t, connect, srv, "Blocker")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:hoglin", 0.5, 64, 1.5)
	srv.SetMobBaby(id, true)

	heard := shieldHolderTraffic(t, c, srv, "Blocker")
	expectHeldBlock(t, heard, "a blocked baby hoglin blow")
	if heard.vz != 0 {
		t.Errorf("the holder's server-side vz = %v, want none", heard.vz)
	}
}

// TestACreeperBlastTheShieldBlockedPushesOnlyByTheBlast: the shield blocks
// the whole blast, so neither its 0.5 shove nor a default knockback reaches
// the holder; only the blast's own push, added to the movement after the
// hurt, does.
// vanilla: LivingEntity.hurtServer (fullyBlocked), LivingEntity.blockedByItem
func TestACreeperBlastTheShieldBlockedPushesOnlyByTheBlast(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := shieldBlocker(t, connect, srv, "Blocker")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:creeper", 0.5, 64, 1.5)

	heard := shieldHolderTraffic(t, c, srv, "Blocker")
	expectHeldBlock(t, heard, "a blocked creeper blast")
	if want := -0.4377; math.Abs(heard.vz-want) > 0.01 {
		t.Errorf("the holder's server-side vz = %v, want the blast's push alone, %v", heard.vz, want)
	}
}

// TestFullyBlockedHitLeavesNoVelocity: a zombie's blow the shield blocks
// leaves nothing in the holder's movement, so a blow from behind the tick
// after sends one stepped knockback away from it. The window the blocked
// blow opened is cleared first: within it vanilla would soften the second
// blow to no knockback at all.
// vanilla: LivingEntity.hurtServer (fullyBlocked), LivingEntity.knockback
func TestFullyBlockedHitLeavesNoVelocity(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	servertest.FreezeUntilHeld(t, srv)
	c := shieldBlocker(t, connect, srv, "Blocker")
	servertest.HoldTicks(t, srv)
	// Past the block delay: a shield just raised blocks nothing yet.
	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	front := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)
	behind := srv.SpawnMob("minecraft:zombie", 0.5, 64, -0.5)
	c.DrainSurvivalTraffic()

	if !srv.StrikePlayerWith(front, "Blocker", 3) {
		t.Fatal("the front zombie or the holder was not found")
	}
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Fatalf("health after the front blow = %v, want 20: the shield did not block it", got)
	}
	if vx, vy, vz := srv.PlayerVelocity("Blocker"); vx != 0 || math.Abs(vy+0.0784) > 1e-6 || vz != 0 {
		t.Errorf("the fully blocked blow left the holder moving at %v, %v, %v, want only resting gravity", vx, vy, vz)
	}
	servertest.RunTicks(t, srv, 1)
	srv.SetHealth("Blocker", 20)
	if !srv.StrikePlayerWith(behind, "Blocker", 3) {
		t.Fatal("the zombie behind or the holder was not found")
	}

	var motion bool
	var vx, vy, vz float64
	c.ReadTicksSkipping(srv, 2, shieldSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetEntityMotion {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				vx, vy, vz = r.ReadLpVec3()
				motion = true
			}
		}
		return motion
	})
	if !motion {
		t.Fatal("the blow from behind sent the holder no Set Entity Motion")
	}
	if math.Abs(vx) > 1e-3 || math.Abs(vy-0.2752) > 0.003 || math.Abs(vz-0.2184) > 0.003 {
		t.Errorf("the blow from behind sent motion %v, %v, %v, want one stepped knockback 0, 0.2752, 0.2184", vx, vy, vz)
	}
}

// TestAHurtOfNothingIsRefused lands a hurt of zero: it opens no window, so
// the next whole hurt still tells a damage event.
// vanilla: Player.hurtServer
func TestAHurtOfNothingIsRefused(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.HostileTestConfig(), "Zero")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	if srv.HurtAs("Zero", 0, rules.DamageTypeMobAttack) {
		t.Fatal("a hurt of zero landed")
	}
	if !srv.HurtAs("Zero", 3, rules.DamageTypeMobAttack) {
		t.Fatal("a hurt of three after the zero did not land")
	}
	var damage bool
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlayDamageEvent && protocol.NewReader(data).ReadVarInt() == id {
			damage = true
		}
	})
	if !damage {
		t.Error("the whole hurt after a zero was held back as a window excess")
	}
}
