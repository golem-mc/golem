package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// With universal_anger a player's blow is not answered by HurtByTargetGoal,
// and a struck piglin angers at the nearest player it sees.
// vanilla: HurtByTargetGoal.canUse, PiglinAi.maybeRetaliate

func TestUniversalAngerKeepsAStruckZombieFromAlertingItsNeighbours(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	srv.SetGameRule("universal_anger", true)
	// Resistance V keeps the player alive while the first zombie hunts.
	c.ChatCommand("effect give @s minecraft:resistance 100 4")
	// The wall hides the player from the second zombie, so only an alert
	// could name its target.
	c.ChatCommand("fill -4 64 4 12 68 4 minecraft:stone")
	c.Settle()
	first := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
	second := srv.SpawnMob("minecraft:zombie", 4.5, 64, 6.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(first)
	c.Settle()
	servertest.RunTicks(t, srv, 20)
	if got := srv.MobTargetID(second); got != 0 {
		t.Errorf("the zombie behind the wall targets %d, want nobody", got)
	}
}

func TestUniversalAngerTurnsAStruckPiglinOnTheNearestPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	srv.SetDayTime(18000)
	srv.SetGameRule("universal_anger", true)
	c := connect()
	c.JoinAs("Slayer")
	c.MoveToGround(2.5, 64, 0.5, true)
	other := connect()
	otherID := other.JoinAs("Other")
	other.MoveToGround(7.5, 64, 0.5, true)
	// Golden boots keep the piglin from hunting either player unprovoked.
	gearedPlayers(c, "Slayer", "Other")
	c.ChatCommand("effect give @a minecraft:resistance 100 4")
	c.Settle()
	other.Settle()
	servertest.HoldTicks(t, srv)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(p)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the piglin targeting the nearer player", func() bool {
		return srv.MobTargetID(p) == otherID
	})
}

// A hoglin's brain answers a player's blow whatever the rule says.
// Invisibility keeps the player 3 blocks away out of the hoglin's own hunt
// (2 blocks at 0.07 of its follow range), which a blow ignores.
// vanilla: HoglinAi.wasHurtBy, LivingEntity.getVisibilityPercent
func TestUniversalAngerStillTurnsAStruckHoglinOnThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := hoglinWorld2(t)
	srv.SetGameRule("universal_anger", true)
	c.ChatCommand("effect give @s minecraft:resistance 100 4")
	c.ChatCommand("effect give @s minecraft:invisibility 100 0")
	c.Settle()
	servertest.HoldTicks(t, srv)
	px := srv.PlayerPosition("Slayer")
	h := srv.SpawnMob(hoglin, px.X+3, 64, px.Z)
	servertest.RunTicks(t, srv, 40)
	if got := srv.MobTargetID(h); got != 0 {
		t.Fatalf("the hoglin targets %d before the blow, want nobody", got)
	}
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(h)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the hoglin targeting the player", func() bool {
		return srv.MobTargetID(h) == playerID
	})
}
