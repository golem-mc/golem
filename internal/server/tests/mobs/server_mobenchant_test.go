package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
