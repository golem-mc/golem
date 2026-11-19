package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnIronGolemSparesAZombieOnItsTeam stands an iron golem 4 blocks from a
// zombie: it targets the zombie within 60 ticks, but never once both are on
// one team, and the zombie never targets the golem then either.
// vanilla: TargetingConditions.test, Entity.isAlliedTo
func TestAnIronGolemSparesAZombieOnItsTeam(t *testing.T) {
	t.Parallel()
	for _, team := range []bool{false, true} {
		srv, _ := golemInteractionScene(t, "Bystander")
		// A creative bystander leaves the golem as the zombie's only prey.
		srv.RunLine("gamemode creative Bystander")
		golem := srv.SpawnMob("minecraft:iron_golem", 6.5, 64, 4.5)
		zombie := srv.SpawnMob("minecraft:zombie", 10.5, 64, 4.5)
		if team {
			srv.RunLine("team add t")
			srv.RunLine("team join t @e[type=minecraft:iron_golem]")
			srv.RunLine("team join t @e[type=minecraft:zombie]")
			for range 60 {
				servertest.RunTicks(t, srv, 1)
				if got := srv.MobTargetID(golem); got != 0 {
					t.Fatalf("the golem targets %d on the zombie's team", got)
				}
				if srv.MobTargetID(zombie) == golem {
					t.Fatal("the zombie targets the golem on its team")
				}
			}
			continue
		}
		servertest.UntilTicks(t, srv, 60, "the golem targeting the zombie", func() bool {
			return srv.MobTargetID(golem) == zombie
		})
	}
}

// TestAZombieStruckByATeammateDoesNotRetaliate has a survival player strike a
// zombie: it targets the player, but not when both are on one team.
// vanilla: HurtByTargetGoal.HURT_BY_TARGETING, TargetGoal.canContinueToUse
func TestAZombieStruckByATeammateDoesNotRetaliate(t *testing.T) {
	t.Parallel()
	for _, team := range []bool{false, true} {
		c, playerID, srv := hoglinWorld2(t)
		zombie := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
		if team {
			srv.RunLine("team add t")
			srv.RunLine("team join t Slayer")
			srv.RunLine("team join t @e[type=minecraft:zombie]")
		}
		c.Settle()
		c.Attack(zombie)
		if !team {
			servertest.UntilTicks(t, srv, 40, "the zombie targeting the player", func() bool {
				return srv.MobTargetID(zombie) == playerID
			})
			continue
		}
		for range 40 {
			servertest.RunTicks(t, srv, 1)
			if got := srv.MobTargetID(zombie); got != 0 {
				t.Fatalf("the struck zombie targets %d on the player's team", got)
			}
		}
	}
}
