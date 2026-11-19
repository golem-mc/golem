package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// customStat reads one of a player's minecraft:custom statistics.
func customStat(read func(name, statType, key string) int32, name, key string) int32 {
	return read(name, registry.StatCustom, key)
}

// TestDamageToAPlayerCountsInTenths: the health a hurt takes counts as
// damage_taken in tenths, after Resistance; the share Absorption soaks counts
// as damage_absorbed and the share Resistance cuts as damage_resisted.
// vanilla: Player.actuallyHurt, LivingEntity.getDamageAfterMagicAbsorb
func TestDamageToAPlayerCountsInTenths(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Testificate")
	stat := func(key string) int32 { return customStat(srv.StatValue, "Testificate", key) }
	reset := func() { srv.OpenHurtWindow("Testificate", 0); srv.SetHealth("Testificate", 20) }

	if !srv.HurtAs("Testificate", 3.14, rules.DamageTypeGeneric) {
		t.Fatal("the hurt did not land")
	}
	if got := stat("minecraft:damage_taken"); got != 31 {
		t.Errorf("damage_taken = %d after a 3.14 hurt, want 31", got)
	}

	// Absorption 4 (amplifier 0) soaks four of a six-point hurt.
	reset()
	srv.GiveEffect("Testificate", "minecraft:absorption", 600, 0, false)
	c.Settle()
	if !srv.HurtAs("Testificate", 6, rules.DamageTypeGeneric) {
		t.Fatal("the hurt did not land")
	}
	if got := stat("minecraft:damage_absorbed"); got != 40 {
		t.Errorf("damage_absorbed = %d, want 40", got)
	}
	if got := stat("minecraft:damage_taken"); got != 31+20 {
		t.Errorf("damage_taken = %d after the absorbed hurt, want 51", got)
	}

	// Resistance I cuts a fifth of a five-point hurt.
	reset()
	srv.GiveEffect("Testificate", "minecraft:resistance", 600, 0, false)
	c.Settle()
	if !srv.HurtAs("Testificate", 5, rules.DamageTypeGeneric) {
		t.Fatal("the hurt did not land")
	}
	if got := stat("minecraft:damage_resisted"); got != 10 {
		t.Errorf("damage_resisted = %d, want 10", got)
	}
	if got := stat("minecraft:damage_taken"); got != 51+40 {
		t.Errorf("damage_taken = %d after the resisted hurt, want 91", got)
	}
}

// TestAShieldCountsTheDamageItBlocks: a frontal blow the raised shield turns
// aside counts as damage_blocked_by_shield in tenths and takes no health.
// vanilla: LivingEntity.hurtServer
func TestAShieldCountsTheDamageItBlocks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	c := shieldBlocker(t, connect, srv, "Blocker")
	srv.StopTick()
	for range 6 {
		srv.TickSurvival()
	}
	if srv.HurtFrom("Blocker", 3, 0.5, 1.5) {
		t.Fatal("the frontal blow was not blocked")
	}
	c.Settle()
	if got := srv.StatValue("Blocker", registry.StatCustom, "minecraft:damage_blocked_by_shield"); got != 30 {
		t.Errorf("damage_blocked_by_shield = %d, want 30", got)
	}
	if got := srv.StatValue("Blocker", registry.StatCustom, "minecraft:damage_taken"); got != 0 {
		t.Errorf("damage_taken = %d after a blocked blow, want 0", got)
	}
}

// TestABlowCountsTheHealthItTook: a sword blow counts the health the mob lost
// as damage_dealt in tenths, and the share its Resistance cut as
// damage_dealt_resisted.
// vanilla: Player.damageStatsAndHearts, LivingEntity.getDamageAfterMagicAbsorb
func TestABlowCountsTheHealthItTook(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		resistance bool
	}{{"plain", false}, {"resistant", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Juggler", servertest.OpsListing(t, "Juggler"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()

			mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			if tc.resistance {
				c.Give("effect give @e[type=minecraft:cow] minecraft:resistance 999 0", mobID)
			}
			srv.GiveSlot("Juggler", player.HotbarSlot, protocol.ItemStack{
				ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
			})
			servertest.AwaitFullSwing(t, srv, "Juggler")
			health := c.Strike(mobID, attacker)
			// Hurt metadata precedes the damage statistic in the attack handler.
			c.Settle()

			wantDealt := int32(math.Round((10 - float64(health)) * 10))
			if got := customStat(srv.StatValue, "Juggler", "minecraft:damage_dealt"); got != wantDealt || got == 0 {
				t.Errorf("damage_dealt = %d, want %d (the cow kept %v)", got, wantDealt, health)
			}
			wantResisted := int32(0)
			if tc.resistance {
				wantResisted = int32(math.Round(0.2 * servertest.SwordDamage() * 10))
			}
			if got := customStat(srv.StatValue, "Juggler", "minecraft:damage_dealt_resisted"); got != wantResisted {
				t.Errorf("damage_dealt_resisted = %d, want %d", got, wantResisted)
			}
		})
	}
}
