package survival

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMaxHealthAttributeCapsHealing heals up to the max_health attribute's
// value rather than the base 20, and a lowered value pulls the health down
// with it. vanilla: LivingEntity.getMaxHealth, LivingEntity.onAttributeUpdated
func TestMaxHealthAttributeCapsHealing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Hale", servertest.OpsListing(t, "Hale"))
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.Settle()

	c.ChatCommand("attribute Hale minecraft:max_health base set 40")
	c.ChatCommand("effect give Hale minecraft:instant_health 1 1")
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	// Instant Health II heals 8 from the full 20.
	if got := srv.PlayerHealth("Hale"); got != 28 {
		t.Errorf("health after Instant Health II under max_health 40 = %v, want 28", got)
	}
	if got := c.HealthReports(); len(got) == 0 || got[len(got)-1] != 28 {
		t.Errorf("health sent after the heal = %v, want it to end at 28", got)
	}

	c.ChatCommand("attribute Hale minecraft:max_health base set 10")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.PlayerHealth("Hale"); got != 10 {
		t.Errorf("health after max_health dropped to 10 = %v, want 10", got)
	}
	if got := c.HealthReports(); len(got) == 0 || got[len(got)-1] != 10 {
		t.Errorf("health sent after the drop = %v, want it to end at 10", got)
	}
}
