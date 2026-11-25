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
	srv.SetMobBaby(p, false) // the seeded spawn roll may make a baby, which hunts no one
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(p)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the piglin targeting the nearer player", func() bool {
		return srv.MobTargetID(p) == otherID
	})
}
