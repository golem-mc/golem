package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"maps"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// meleeVictim stands a zombie holding stack beside a fresh survival player
// on normal difficulty, where a blow is not scaled, and returns the health
// the first blow left and the player's fire ticks right after it.
func meleeVictim(t *testing.T, held protocol.ItemStack) (health float64, fire int) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyNormal)
	c := connect().Join("Target")

	id := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	srv.SetMobHand(id, held)

	pumpTicks(c, srv, 400, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				health = float64(h.Health)
				return true
			}
		}
		return false
	})
	return health, srv.PlayerFire("Target")
}

// TestAZombiesSwordAddsItsSharpness: Sharpness V adds 0.5*5+0.5 = 3 to the
// blow before the difficulty scales it.
// vanilla: Mob.doHurtTarget, EnchantmentHelper.modifyDamage
func TestAZombiesSwordAddsItsSharpness(t *testing.T) {
	t.Parallel()
	sword := servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sharpness", 5)
	plain := protocol.ItemStack{ItemID: sword.ItemID, Count: 1}
	base := 20 - (3 + float64(registry.AttackDamage(sword.ItemID)))
	if got, _ := meleeVictim(t, plain); got != base {
		t.Fatalf("plain sword left %v health, want %v", got, base)
	}
	if got, _ := meleeVictim(t, sword); got != base-3 {
		t.Errorf("sharpness V sword left %v health, want %v", got, base-3)
	}
}

// TestAZombiesFireAspectSwordIgnitesThePlayer: the post-attack ignite.
// vanilla: EnchantmentHelper.doPostAttackEffects
func TestAZombiesFireAspectSwordIgnitesThePlayer(t *testing.T) {
	t.Parallel()
	_, fire := meleeVictim(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:fire_aspect", 2))
	if fire <= 0 {
		t.Error("a fire aspect zombie's blow left the player not burning")
	}
	if _, fire := meleeVictim(t, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:iron_sword"), Count: 1}); fire > 0 {
		t.Errorf("a plain sword's blow left the player burning for %d ticks", fire)
	}
}

// TestASkeletonsFlameBowShootsBurningArrows: the arrow keeps the bow it
// was fired from. vanilla: AbstractSkeleton.performRangedAttack
func TestASkeletonsFlameBowShootsBurningArrows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		bow  protocol.ItemStack
		want bool
	}{
		{"flame", servertest.EnchantedStack(t, "minecraft:bow", "minecraft:flame", 1), true},
		{"plain", protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BowTestConfig())
			srv.SetDayTime(18000)
			c := connect()
			c.JoinAs("Prey")
			id := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
			srv.SetMobHand(id, tc.bow)

			var shot []server.TestArrow
			servertest.UntilTicks(t, srv, 600, "the skeleton shot", func() bool {
				shot = srv.ArrowsInDimension("overworld")
				return len(shot) > 0
			})
			if shot[0].Burning != tc.want {
				t.Errorf("arrow burning = %v, want %v", shot[0].Burning, tc.want)
			}
		})
	}
}

// TestASkeletonsArrowCarriesItsBowsPowerAndPunch: the shaft keeps the bow, so
// the Power and Punch read at the hit are the bow's.
// vanilla: AbstractSkeleton.performRangedAttack
func TestASkeletonsArrowCarriesItsBowsPowerAndPunch(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	srv.SetDayTime(18000)
	c := connect()
	c.JoinAs("Prey")
	id := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	bow := servertest.EnchantedStack(t, "minecraft:bow", "minecraft:power", 5)
	maps.Copy(bow.Enchantments,
		servertest.EnchantedStack(t, "minecraft:bow", "minecraft:punch", 2).Enchantments)
	srv.SetMobHand(id, bow)

	var shot []server.TestArrow
	servertest.UntilTicks(t, srv, 600, "the skeleton shot", func() bool {
		shot = srv.ArrowsInDimension("overworld")
		return len(shot) > 0
	})
	ctx := &rules.EnchContext{
		DamageType: rules.DamageTypeArrow, Victim: protocol.EntityTypePlayer,
		DirectAttacker: protocol.EntityTypeArrow,
	}
	if got := rules.EnchantedDamage(shot[0].Weapon, ctx, 2); got <= 2 {
		t.Errorf("the arrow's weapon turns 2 damage into %v, want Power to add to it", got)
	}
	if got := rules.EnchantedKnockback(shot[0].Weapon, ctx, 0); got != 2 {
		t.Errorf("the arrow's weapon gives knockback %v, want Punch II's 2", got)
	}
}

// strikeShoves strikes a player on the ground with the zombie holding sword
// (after a prior hurt of the given size when opening is positive, which
// leaves the invulnerability window open) and returns the shoves the player
// was sent.
func strikeShoves(t *testing.T, sword protocol.ItemStack, opening float64) [][3]float64 {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyNormal)
	playerID := c.JoinAs("Target")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
	srv.SetMobHand(id, sword)
	if opening > 0 {
		srv.Hurt("Target", opening)
	}
	srv.SetPlayerVelocity("Target", 0, 0, 0)
	if !srv.StrikePlayerWith(id, "Target", 3) {
		t.Fatal("the strike found no mob or player")
	}
	var out [][3]float64
	c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == playerID {
			vx, vy, vz := r.ReadLpVec3()
			out = append(out, [3]float64{vx, vy, vz})
		}
		return false
	})
	return out
}

// TestAZombiesKnockbackSwordShovesAlongItsYaw: the default shove from the
// zombie and the extra along its facing, folded as a player's blow is.
// vanilla: Mob.doHurtTarget, LivingEntity.knockback
func TestAZombiesKnockbackSwordShovesAlongItsYaw(t *testing.T) {
	t.Parallel()
	shoves := strikeShoves(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 2), 0)
	if len(shoves) == 0 {
		t.Fatal("the knockback blow shoved nobody")
	}
	vx, vy, vz := rules.KnockbackExtra(0.5-4.5, 0, 0, 1.0, 1, true, rand.New(rand.NewSource(1)))
	vx, vy, vz = vx*0.546, (vy-0.08)*0.98, vz*0.546
	got := shoves[0]
	if math.Abs(got[0]-vx) > 0.01 || math.Abs(got[1]-vy) > 0.01 || math.Abs(got[2]-vz) > 0.01 {
		t.Errorf("shove = %v, want (%v, %v, %v)", got, vx, vy, vz)
	}
}

// TestAZombiesKnockbackSwordShovesThroughTheWindow: a blow that lands only
// its excess through the invulnerability window skips the default shove but
// keeps the weapon's extra, and a plain sword shoves nobody then.
// vanilla: Player.causeExtraKnockback
func TestAZombiesKnockbackSwordShovesThroughTheWindow(t *testing.T) {
	t.Parallel()
	if got := strikeShoves(t, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:iron_sword"), Count: 1}, 1); len(got) != 0 {
		t.Errorf("a plain sword's window-only blow shoved %v", got)
	}
	shoves := strikeShoves(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 2), 1)
	if len(shoves) != 1 {
		t.Fatalf("the window-only knockback blow shoved %d times, want 1", len(shoves))
	}
	vx, vy, vz := rules.KnockbackExtraOnly(0, 1.0, 1, true)
	vx, vy, vz = vx*0.546, (vy-0.08)*0.98, vz*0.546
	got := shoves[0]
	if math.Abs(got[0]-vx) > 0.01 || math.Abs(got[1]-vy) > 0.01 || math.Abs(got[2]-vz) > 0.01 {
		t.Errorf("shove = %v, want (%v, %v, %v)", got, vx, vy, vz)
	}
}

// mobVictimOf strikes a cow with the zombie holding held and returns
// the cow's health, velocity and fire ticks after it.
func mobVictimOf(t *testing.T, held protocol.ItemStack) (health, vx, vz float64, fire int) {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyNormal)
	c.JoinAs("Watcher")
	servertest.HoldTicks(t, srv)
	zombie := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
	cow := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	srv.SetMobHand(zombie, held)
	if !srv.StrikeMobWith(zombie, cow, 3) {
		t.Fatal("the strike found no mobs")
	}
	health, _ = srv.MobHealth(cow)
	vx, _, vz, _ = srv.MobVelocity(cow)
	return health, vx, vz, srv.MobFireTicks(cow)
}

// TestAZombiesWeaponEnchantsItsBlowOnAMob: Sharpness, Knockback and Fire
// Aspect apply to a blow on any body, not only a player's.
// vanilla: Mob.doHurtTarget
func TestAZombiesWeaponEnchantsItsBlowOnAMob(t *testing.T) {
	t.Parallel()
	plain := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:iron_sword"), Count: 1}
	baseHealth, baseVX, baseVZ, baseFire := mobVictimOf(t, plain)
	if baseFire != 0 || math.Abs(baseVZ) > 0.01 {
		t.Fatalf("a plain sword left fire %d and a sideways shove %v", baseFire, baseVZ)
	}
	sharp, _, _, _ := mobVictimOf(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sharpness", 5))
	if sharp != baseHealth-3 {
		t.Errorf("sharpness V left %v health, want %v", sharp, baseHealth-3)
	}
	_, kx, kz, _ := mobVictimOf(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 2))
	if math.Abs(kz-1.0) > 0.01 || math.Abs(kx-baseVX/2) > 0.01 {
		t.Errorf("knockback II shoved (%v, %v), want (%v, 1)", kx, kz, baseVX/2)
	}
	_, _, _, fire := mobVictimOf(t, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:fire_aspect", 2))
	if fire <= 0 {
		t.Error("a fire aspect blow left the mob not burning")
	}
}
