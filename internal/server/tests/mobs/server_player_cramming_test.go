package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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

// twoPlayerCrammingScene joins Stayer and Bystander, both in the spawn chunk,
// before the loop is held, with Stayer at x=11.5 and the rule at limit.
func twoPlayerCrammingScene(t *testing.T, limit int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Stayer", servertest.OpsListing(t, "Stayer"))
	other := servertest.DialServer(t, srv).Join("Bystander")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(11.5, 64, 0.5, true)
	c.Settle()
	other.MoveToGround(14.5, 64, 12.5, true)
	other.Settle()
	srv.SetGameRule("natural_health_regeneration", false)
	srv.SetGameRuleValue("max_entity_cramming", limit)
	return srv, c
}

// TestCrammingIgnoresAMobOnlyAnotherPlayerReaches: a scaled cow whose box
// overlaps a player but is filed beyond that player's sections is not
// counted, though another player's search reaches it.
// vanilla: LivingEntity.pushEntities, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestCrammingIgnoresAMobOnlyAnotherPlayerReaches(t *testing.T) {
	t.Parallel()
	srv, c := twoPlayerCrammingScene(t, 1)
	cow := srv.SpawnMob("minecraft:cow", 18, 64, 0.5)
	srv.SetMobNoAI(cow)
	c.ChatCommand("attribute @e[type=minecraft:cow,limit=1] minecraft:scale base set 16")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	servertest.RunTicks(t, srv, 20)
	if hp := srv.PlayerHealth("Stayer"); hp != 20 {
		t.Fatalf("health = %v, want 20: the cow is filed outside the player's sections", hp)
	}
}

// TestCrammingCountsAMobBothPlayersReachOnce: one cow in a chunk both
// players search counts once against a limit of two.
// vanilla: LivingEntity.pushEntities
func TestCrammingCountsAMobBothPlayersReachOnce(t *testing.T) {
	t.Parallel()
	srv, _ := twoPlayerCrammingScene(t, 2)
	cow := srv.SpawnMob("minecraft:cow", 11.5, 64, 0.5)
	srv.SetMobNoAI(cow)
	servertest.RunTicks(t, srv, 20)
	if hp := srv.PlayerHealth("Stayer"); hp != 20 {
		t.Fatalf("health = %v, want 20: the cow was counted twice", hp)
	}
}
