package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A player in the crowd takes the cramming hurt like a mob does.
// vanilla: LivingEntity.pushEntities

// crammedPlayer stands a survival player among six held pigs at the rule's
// limit, runs 60 ticks and returns the health lost and the player's id.
func crammedPlayer(t *testing.T, limit int32) (lost float64, c *servertest.Client, id int32) {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Crowded", servertest.OpsListing(t, "Crowded"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetGameRule("natural_health_regeneration", false)
	srv.SetGameRuleValue("max_entity_cramming", limit)
	for range 6 {
		pig := srv.SpawnMob("minecraft:pig", 0.5, 64, 0.5)
		if !srv.SetMobNoAI(pig) {
			t.Fatal("a pig could not be held")
		}
	}
	servertest.RunTicks(t, srv, 60)
	return 20 - srv.PlayerHealth("Crowded"), c, id
}

// TestPlayerTakesCrammingDamage: six bodies at a limit of two hurt the player
// in blows of 6, announced as cramming.
func TestPlayerTakesCrammingDamage(t *testing.T) {
	t.Parallel()
	lost, c, id := crammedPlayer(t, 2)
	if lost <= 0 || math.Mod(lost, 6) != 0 {
		t.Fatalf("health lost = %v, want a positive multiple of 6", lost)
	}
	want := registry.MustID("minecraft:damage_type", "minecraft:cramming")
	hit := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayDamageEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id && r.ReadVarInt() == want {
			hit = true
		}
	})
	if !hit {
		t.Error("no cramming damage event for the player")
	}
}

// TestPlayerIgnoresCrammingWhenRuleIsZero: the rule at 0 turns the hurt off.
func TestPlayerIgnoresCrammingWhenRuleIsZero(t *testing.T) {
	t.Parallel()
	if lost, _, _ := crammedPlayer(t, 0); lost != 0 {
		t.Fatalf("health lost = %v, want 0", lost)
	}
}
