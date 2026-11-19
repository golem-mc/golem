package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
)

// The strength and weakness effects on a player's melee blow: the blow is
// the attack_damage attribute, so an effect's modifier rides it and every
// caller - a bare fist, a blade, and the sweep the blade raises - reads the
// same number. The zombie's natural two armour points soften what lands, so
// the health each test wants is the blow put through DamageAfterArmour.

// TestStrengthRaisesABareFistsBlow puts a player under Strength II and
// swings an empty hand at a zombie: the blow is the fist's one plus the
// effect's three a level, seven off the zombie's twenty.
func TestStrengthRaisesABareFistsBlow(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Bruiser")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.GiveEffect("Bruiser", "minecraft:strength", 60, 1, false)
	servertest.AwaitFullSwing(t, srv, "Bruiser")
	c.AwaitMobEffect()

	srv.StopTick()
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	want := float32(20 - rules.DamageAfterArmour(7, 2, 0))
	if got := c.Strike(zombie, attacker); math.Abs(float64(got-want)) > 0.01 {
		t.Errorf("a bare fist under Strength II left the zombie at %v, want %v (the seven of the fist's one and the effect's six)", got, want)
	}
}

// TestWeaknessLowersASwordsBlow puts a player under Weakness I and swings a
// diamond sword at a zombie: the blade's seven is cut by the effect's four
// a level.
func TestWeaknessLowersASwordsBlow(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Hexed")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.GiveSlot("Hexed", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	srv.GiveEffect("Hexed", "minecraft:weakness", 60, 0, false)
	servertest.AwaitFullSwing(t, srv, "Hexed")
	c.AwaitMobEffect()

	srv.StopTick()
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	want := float32(20 - rules.DamageAfterArmour(7-4, 2, 0))
	if got := c.Strike(zombie, attacker); math.Abs(float64(got-want)) > 0.01 {
		t.Errorf("a diamond sword under Weakness I left the zombie at %v, want %v (the blade's seven less the effect's four)", got, want)
	}
}

// TestASweepCarriesTheEffectBlow plants a Sweeping Edge III blade in the
// hand of a player under Strength II and swings at a zombie with a second
// beside it: the stricken zombie takes the sword's twelve, and the swept
// one takes the sweep's plain one plus three quarters of that same twelve
// - the effect's six reaching the sweep through the blow it is cut from.
func TestASweepCarriesTheEffectBlow(t *testing.T) {
	t.Parallel()
	f := newSweepFixture(t)
	f.watcher.MoveToGround(40.5, 64, 40.5, true)
	f.watcher.Settle()
	f.srv.GiveSlot("Fencer", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sweeping_edge", 3))
	f.srv.GiveEffect("Fencer", "minecraft:strength", 60, 1, false)
	servertest.AwaitFullSwing(t, f.srv, "Fencer")

	f.srv.StopTick()
	stricken := f.srv.SpawnMob("minecraft:zombie", 4.5, 64, 5.5)
	beside := f.srv.SpawnMob("minecraft:zombie", 5.45, 64, 5.5)
	f.fencer.Settle()
	f.watcher.Settle()

	f.fencer.Attack(stricken)
	f.fencer.Settle()
	heard := f.watcher.HearBodies()

	wantStricken := float32(20 - rules.DamageAfterArmour(12, 2, 0))
	if got := heard.Healths[stricken]; math.Abs(float64(got-wantStricken)) > 0.01 {
		t.Errorf("the stricken zombie was left at %v, want %v (the sword's twelve)", got, wantStricken)
	}
	wantBeside := float32(20 - rules.DamageAfterArmour(1+0.75*12, 2, 0))
	if got := heard.Healths[beside]; math.Abs(float64(got-wantBeside)) > 0.01 {
		t.Errorf("the swept zombie was left at %v, want %v (one plus three quarters of the twelve)", got, wantBeside)
	}
}

// TestAWeaknessFistOnAPlayerLandsNothing swings a bare fist, under Weakness
// I, at another player: the fist's one less the effect's four clamps to
// nought, and vanilla skips a blow with neither damage nor a magic boost,
// so none of a landed hit's tells — no damage event, no flinch, no health —
// reaches the victim.
func TestAWeaknessFistOnAPlayerLandsNothing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())

	striker := connect()
	striker.JoinAs("Weakling")
	striker.MoveToGround(2.5, 64, 5.5, true)
	striker.Settle()

	target := connect()
	target.JoinAs("Target")
	targetID := striker.ExpectPlayerAdded("Target")
	target.MoveToGround(4.5, 64, 5.5, true)
	target.Settle()
	striker.Settle()

	srv.GiveEffect("Weakling", "minecraft:weakness", 60, 0, false)
	servertest.AwaitFullSwing(t, srv, "Weakling")
	striker.Settle()

	servertest.HoldTicks(t, srv)
	striker.Attack(targetID)
	striker.ReadTicks(srv, 10, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation:
			striker.T.Fatalf("a zero-damage fist was answered with packet 0x%02X (% X)", gotID, data)
		}
		return false
	})
	if got := srv.PlayerHealth("Target"); math.Abs(got-20) > 0.01 {
		striker.T.Errorf("the target was left at %v of twenty, want the zero blow to leave it whole", got)
	}
}

// TestAWeaknessFistOnAMobLandsNothing swings the same clamped-to-nought fist
// at a mob: none of a landed blow's feedback plays, not even the nodamage
// sound a mob's refused hurt answers with.
func TestAWeaknessFistOnAMobLandsNothing(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveEffect("Fencer", "minecraft:weakness", 60, 0, false)
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := feedbackCow(srv, attacker, watcher)

	attacker.Attack(cow)
	for _, c := range []*servertest.Client{attacker, watcher} {
		h := c.HearBlow(cow)
		if len(h.Sounds) != 0 {
			t.Errorf("a zero-damage fist played %v, want no attack sound at all", h.Sounds)
		}
		if len(h.Animations) != 0 {
			t.Errorf("a zero-damage fist animated the cow with %v", h.Animations)
		}
	}
}

// TestAWeaknessFistOnABoatLandsNothing swings the clamped fist at a boat:
// the blow gives no feedback, so none of a landed blow's tells plays.
func TestAWeaknessFistOnABoatLandsNothing(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	srv.GiveEffect("Fencer", "minecraft:weakness", 60, 0, false)
	servertest.AwaitFullSwing(t, srv, "Fencer")
	srv.StopTick()
	boat := srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	attacker.Settle()
	watcher.Settle()

	attacker.Attack(boat)
	for _, c := range []*servertest.Client{attacker, watcher} {
		if h := c.HearBlow(boat); len(h.Sounds) != 0 {
			t.Errorf("a zero-damage fist on a boat played %v, want no attack sound at all", h.Sounds)
		}
	}
}
