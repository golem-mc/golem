package combat

import (
	"testing"

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
