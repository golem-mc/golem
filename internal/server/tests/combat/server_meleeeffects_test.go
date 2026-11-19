package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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
