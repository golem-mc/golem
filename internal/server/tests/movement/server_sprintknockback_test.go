package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// The sprint's knockback: a full-strength blow landed while running shoves
// its victim harder than the same blow standing, the half a block a tick
// vanilla's Player.Attack adds, and a hit whose two stand at the same x and
// z is still pushed somewhere. Each drives a real connection against the
// flat world the harness grows, the way the mob and combat tests do.

// TestAStandingHitShovesTheHurtsOwnKnockback lands a full bare-hand swing on
// a zombie while standing: the shove is the hurt's own four tenths. Each of
// this and the sprinting test keeps one live zombie, so no second body's
// metadata interleaves with the packets read. vanilla: LivingEntity.knockback.
func TestAStandingHitShovesTheHurtsOwnKnockback(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Walker", servertest.OpsListing(t, "Walker"))
	// Facing east, the way the zombie is, so the shove's two directions line
	// up and the speed is the amount alone.
	c.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Walker")
	// The loop stops and a NoAI zombie stands still, so the shove reads the
	// hurt's own four tenths exactly rather than half a step it carried into
	// the blow.
	srv.StopTick()
	c.ChatCommand("summon minecraft:zombie 5.5 64 5.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	srv.SetMobKnockbackResistance(mob, 0)
	if got := c.StrikeMobMotion(mob, attacker); math.Abs(got-0.4) > 0.01 {
		t.Errorf("a standing bare-hand blow shoved the zombie at %v, want the hurt's own 0.4", got)
	}
}

// TestASprintingHitAddsTheKnockback lands the same full swing while sprinting
// and holds the shove the harder: the running blow folds the hurt's four
// tenths with the attack's knockback, a half, to seven tenths. vanilla:
// Player.Attack, LivingEntity.knockback.
func TestASprintingHitAddsTheKnockback(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Runner")
	c.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	// The loop stops before the zombie stands up, so its first steps cannot
	// fold into the blow: half a tick of walk reads short of the seven tenths.
	srv.StopTick()
	mob := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	// A zombie's random spawn resistance would shave the exact push.
	srv.SetMobKnockbackResistance(mob, 0)
	c.SendSprint(attacker, true)
	if got := c.StrikeMobMotion(mob, attacker); math.Abs(got-0.7) > 0.01 {
		t.Errorf("a sprinting bare-hand blow shoved the zombie at %v, want the folded 0.7", got)
	}
}

// TestASprintingHitShovesAlongTheAttackerFacing turns the attacker 90° away
// from the victim and holds the extra shove on the attacker's look, not the
// line to the victim: the hurt's own four tenths, halved by the attack's
// second call to two, runs east along the line, while the attack's half runs
// south along the facing. vanilla: Player.causeExtraKnockback.
func TestASprintingHitShovesAlongTheAttackerFacing(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Turner", servertest.OpsListing(t, "Turner"))
	// The zombie due east, the attacker looking south (yaw 0).
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Turner")
	// The loop stops and a NoAI zombie stands still, so the two shoves fold
	// over a body carrying no velocity of its own.
	srv.StopTick()
	c.ChatCommand("summon minecraft:zombie 5.5 64 5.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	srv.SetMobKnockbackResistance(mob, 0)
	c.SendSprint(attacker, true)
	vx, vz := c.StrikeMobVelocity(mob, attacker)
	if math.Abs(vx-0.2) > 0.01 || math.Abs(vz-0.5) > 0.01 {
		t.Errorf("the sprinting shove = (%v, %v), want the folded (0.2, 0.5)", vx, vz)
	}
}

// TestASprintingPvPHitAddsTheKnockback lands the same pair of blows on a
// player: a full bare-hand swing standing shoves the victim four tenths,
// while the sprinting one adds the attack's half through the fold to seven.
func TestASprintingPvPHitAddsTheKnockback(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attackerID := attacker.JoinAs("Runner")
	victim.ExpectPlayerAdded("Runner")

	// Facing east, the way the victim is, so the shove's two directions line
	// up and the speed is the amount alone.
	attacker.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	if got := victim.NextPlayerMotion(); math.Abs(got-0.4) > 0.01 {
		t.Errorf("a standing bare-hand blow shoved the victim at %v, want the hurt's own 0.4", got)
	}

	servertest.AwaitFullSwing(t, srv, "Runner")
	attacker.SendSprint(attackerID, true)
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	if got := victim.NextPlayerMotion(); math.Abs(got-0.7) > 0.01 {
		t.Errorf("a sprinting bare-hand blow shoved the victim at %v, want the folded 0.7", got)
	}
}

// TestASameSpotHitStillPushes stands the victim at the attacker's own x and
// z, where the hit has no away to push along, and holds that vanilla's
// knockback still deals its horizontal four tenths by drawing a direction.
func TestASameSpotHitStillPushes(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Stacker")
	c.MoveToGround(20.5, 64, 20.5, true)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Stacker")
	// The loop stops so nothing pushes the two bodies apart before the blow.
	srv.StopTick()

	mob := srv.SpawnMob("minecraft:zombie", 20.5, 64, 20.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.SetMobKnockbackResistance(mob, 0)
	if got := c.StrikeMobMotion(mob, attacker); math.Abs(got-0.4) > 0.01 {
		t.Errorf("a same-spot blow shoved the zombie at %v, want the random direction's 0.4", got)
	}
}

// nextMetadataOf returns the flags of the next entity metadata packet that
// names id, skipping whatever else arrives first.
func nextMetadataOf(c *servertest.Client, id int32) (uint8, bool) {
	c.T.Helper()
	for pid, data := range c.Packets(servertest.ExpectTimeout) {
		if pid != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != id {
			continue
		}
		return servertest.ParseMetadata(c.T, protocol.NewReader(data)).Flags, true
	}
	return 0, false
}

// TestAKnockbackHitEndsTheAttackersSprint runs one sprinting blow and holds
// that the attacker's sprint flag drops in a broadcast of its own metadata
// and that the blow after it, the client never having sent Stop Sprinting,
// gets no sprint bonus. vanilla: Player.causeExtraKnockback
func TestAKnockbackHitEndsTheAttackersSprint(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attackerID := attacker.JoinAs("Runner")
	victim.ExpectPlayerAdded("Runner")

	attacker.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	attacker.SendSprint(attackerID, true)
	if got := attacker.ExpectMetadata(); got.Flags&protocol.EntityFlagSprinting == 0 {
		t.Fatalf("entity flags = %#02x, want the sprinting bit set", got.Flags)
	}
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	if got := victim.NextPlayerMotion(); math.Abs(got-0.7) > 0.01 {
		t.Fatalf("a sprinting blow shoved the victim at %v, want the folded 0.7", got)
	}
	if got, ok := nextMetadataOf(attacker, attackerID); !ok || got&protocol.EntityFlagSprinting != 0 {
		t.Errorf("entity flags = %#02x (sent %v) after the blow, want the sprinting bit clear", got, ok)
	}

	servertest.AwaitFullSwing(t, srv, "Runner")
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	if got := victim.NextPlayerMotion(); math.Abs(got-0.4) > 0.01 {
		t.Errorf("the blow after a sprinting one shoved the victim at %v, want the hurt's own 0.4", got)
	}
}

// TestAKnockbackHitOnAMobEndsTheAttackersSprint is the mob half of the
// above: the sprint ends when the blow lands on a mob.
func TestAKnockbackHitOnAMobEndsTheAttackersSprint(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Runner")
	c.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	srv.StopTick()
	mob := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.SetMobKnockbackResistance(mob, 0)
	c.SendSprint(attacker, true)
	if got := c.ExpectMetadata(); got.Flags&protocol.EntityFlagSprinting == 0 {
		t.Fatalf("entity flags = %#02x, want the sprinting bit set", got.Flags)
	}
	c.StrikeMobMotion(mob, attacker)
	if got, ok := nextMetadataOf(c, attacker); !ok || got&protocol.EntityFlagSprinting != 0 {
		t.Errorf("entity flags = %#02x (sent %v) after the blow, want the sprinting bit clear", got, ok)
	}
}

// TestAKnockbackHitOnABoatEndsTheAttackersSprint: a sprinting blow on a boat
// carries the 0.5 bonus too, and Player.causeExtraKnockback ends the sprint
// and pushes the boat, a target that is not living, along the attacker's yaw.
// vanilla: Player.causeExtraKnockback, Entity.push
func TestAKnockbackHitOnABoatEndsTheAttackersSprint(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Runner")
	c.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	srv.StopTick()
	boat := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	c.SendSprint(attacker, true)
	if got, ok := nextMetadataOf(c, attacker); !ok || got&protocol.EntityFlagSprinting == 0 {
		t.Fatalf("entity flags = %#02x (sent %v), want the sprinting bit set", got, ok)
	}
	c.Attack(boat)
	if got, ok := nextMetadataOf(c, attacker); !ok || got&protocol.EntityFlagSprinting != 0 {
		t.Errorf("entity flags = %#02x (sent %v) after the blow, want the sprinting bit clear", got, ok)
	}
	// Facing east (yaw -90): the 0.5 bonus along +x and a tenth up.
	vx, vy, vz, ok := srv.BoatVelocity(boat)
	if !ok {
		t.Fatal("the boat broke under one blow")
	}
	if math.Abs(vx-0.5) > 1e-6 || math.Abs(vy-0.1) > 1e-6 || math.Abs(vz) > 1e-6 {
		t.Errorf("boat velocity after the blow = (%v, %v, %v), want (0.5, 0.1, 0)", vx, vy, vz)
	}
	srv.TickBoats(3)
	x, _, z, _, _, ok := srv.BoatState(boat)
	if !ok {
		t.Fatal("the boat is gone three ticks after the blow")
	}
	if x <= 5.6 || math.Abs(z-5.5) > 1e-6 {
		t.Errorf("boat at x=%v z=%v three ticks after the blow, want pushed east of 5.5 along z=5.5", x, z)
	}
}

// TestAKnockbackHitOnAMinecartPushesIt: a sprinting full-strength blow on a
// cart pushes it along the attacker's yaw without breaking it.
// vanilla: Player.causeExtraKnockback, Entity.push
func TestAKnockbackHitOnAMinecartPushesIt(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Runner")
	c.MoveToGrounded(2.5, 64, 5.5, -90, 0, true)
	c.Settle()

	servertest.AwaitFullSwing(t, srv, "Runner")
	srv.StopTick()
	cart := srv.SpawnMinecart(5.5, 64, 5.5)
	c.SendSprint(attacker, true)
	if got, ok := nextMetadataOf(c, attacker); !ok || got&protocol.EntityFlagSprinting == 0 {
		t.Fatalf("entity flags = %#02x (sent %v), want the sprinting bit set", got, ok)
	}
	c.Attack(cart)
	if got, ok := nextMetadataOf(c, attacker); !ok || got&protocol.EntityFlagSprinting != 0 {
		t.Fatalf("entity flags = %#02x (sent %v) after the blow, want the sprinting bit clear", got, ok)
	}
	srv.TickMinecarts(3)
	x, _, z, _, ok := srv.MinecartState(cart)
	if !ok {
		t.Fatal("the cart broke under one blow")
	}
	if x <= 5.6 || math.Abs(z-5.5) > 1e-6 {
		t.Errorf("cart at x=%v z=%v three ticks after the blow, want pushed east of 5.5 along z=5.5", x, z)
	}
}

// TestASpearJabEndsTheJabbersSprint: a jab that deals knockback runs
// causeExtraKnockback(0.4) on each body it meets, ending the sprint.
// vanilla: Player.stabAttack
func TestASpearJabEndsTheJabbersSprint(t *testing.T) {
	t.Parallel()
	srv, c, jabber := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1})
	srv.TickSurvival()
	srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	c.SendSprint(jabber, true)
	if got, ok := nextMetadataOf(c, jabber); !ok || got&protocol.EntityFlagSprinting == 0 {
		t.Fatalf("entity flags = %#02x (sent %v), want the sprinting bit set", got, ok)
	}
	servertest.ChargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	if got, ok := nextMetadataOf(c, jabber); !ok || got&protocol.EntityFlagSprinting != 0 {
		t.Errorf("entity flags = %#02x (sent %v) after the jab, want the sprinting bit clear", got, ok)
	}
}
