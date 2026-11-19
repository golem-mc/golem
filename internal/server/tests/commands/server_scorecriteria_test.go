package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// addObjective makes an objective through the command and reads its feedback.
func addObjective(t *testing.T, c *servertest.Client, name, criteria string) {
	t.Helper()
	c.ChatCommand("scoreboard objectives add " + name + " " + criteria)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
}

// waitScore ticks until the holder's score in the objective is want.
func waitScore(t *testing.T, srv *server.Server, holder, objective string, want int32) {
	t.Helper()
	servertest.UntilTicks(t, srv, 60, objective+" of "+holder, func() bool {
		v, ok := srv.ScoreOf(holder, objective)
		return ok && v == want
	})
}

// TestPlayerCriteriaFollowThePlayer checks that the health, food, level and
// armor, xp and air objectives are written when the player's value changes,
// and that a death bumps deathCount while the respawn writes the new body's
// values again.
// vanilla: ServerPlayer.doTick, ServerPlayer.die
func TestPlayerCriteriaFollowThePlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	for _, name := range []string{"health", "food", "level", "armor", "xp", "air"} {
		addObjective(t, c, "o_"+name, name)
	}
	addObjective(t, c, "o_deaths", "deathCount")

	srv.SetFood("Testificate", 7)
	waitScore(t, srv, "Testificate", "o_food", 7)
	srv.SetExperienceLevels("Testificate", 5)
	waitScore(t, srv, "Testificate", "o_level", 5)
	c.ChatCommand("experience add Testificate 3 points")
	c.Settle()
	waitScore(t, srv, "Testificate", "o_xp", 3)

	srv.SetHealth("Testificate", 5.5)
	waitScore(t, srv, "Testificate", "o_health", 6)

	srv.GiveSlot("Testificate", player.ArmorSlot+1, servertest.ItemStack(t, "minecraft:diamond_chestplate", 1))
	waitScore(t, srv, "Testificate", "o_armor", 8)

	// The respawn is a new body: every objective is written again, the air
	// one for the first time. The body lands on the experience orbs the
	// death dropped and picks them up one tick after the first write, so the
	// reset level is only there for one tick: hold the loop to read it.
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.KillAndRespawn(t, c, c.PlayerEntityID, "Testificate")
	waitScore(t, srv, "Testificate", "o_deaths", 1)
	waitScore(t, srv, "Testificate", "o_health", 20)
	waitScore(t, srv, "Testificate", "o_food", 20)
	waitScore(t, srv, "Testificate", "o_level", 0)
	waitScore(t, srv, "Testificate", "o_armor", 0)
	waitScore(t, srv, "Testificate", "o_air", 300)
}

// TestKillCountsFollowTheKiller checks that a player's blow on a mob bumps
// totalKillCount alone and a blow on a player bumps playerKillCount too.
// vanilla: ServerPlayer.awardKillScore
func TestKillCountsFollowTheKiller(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Killer"))
	killer := connect()
	killer.JoinAs("Killer")
	victim := connect()
	victimID := victim.JoinAs("Victim")
	killer.MoveToGround(2.5, 64, 5.5, true)
	killer.MoveToGround(2.5, 64, 5.5, true)
	victim.MoveToGround(2.5, 64, 6.5, true)
	victim.Settle()
	killer.Settle()
	addObjective(t, killer, "total", "totalKillCount")
	addObjective(t, killer, "players", "playerKillCount")
	srv.GiveSlot("Killer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 3))

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	killer.Settle()
	servertest.AwaitFullSwing(t, srv, "Killer")
	killer.Attack(cow)
	waitScore(t, srv, "Killer", "total", 1)
	if v, ok := srv.ScoreOf("Killer", "players"); ok && v != 0 {
		t.Errorf("playerKillCount = %d after a cow, want none", v)
	}
	// The statistics follow the victim as the objectives do.
	stat := func(key string) int32 { return srv.StatValue("Killer", registry.StatCustom, key) }
	if mobs, players := stat("minecraft:mob_kills"), stat("minecraft:player_kills"); mobs != 1 || players != 0 {
		t.Errorf("after a cow mob_kills = %d, player_kills = %d, want 1 and 0", mobs, players)
	}

	servertest.AwaitFullSwing(t, srv, "Killer")
	srv.SetHealth("Victim", 1)
	killer.Attack(victimID)
	waitScore(t, srv, "Killer", "total", 2)
	waitScore(t, srv, "Killer", "players", 1)
	if mobs, players := stat("minecraft:mob_kills"), stat("minecraft:player_kills"); mobs != 1 || players != 1 {
		t.Errorf("after a player mob_kills = %d, player_kills = %d, want 1 and 1", mobs, players)
	}
}

// TestTeamKillCriteriaFollowTheTeams checks that a kill bumps the killer in
// teamkill.<victim's team colour> and the victim, by UUID for a mob, in
// killedByTeam.<killer's team colour>.
// vanilla: ServerPlayer.awardKillScore, ServerPlayer.handleTeamKill
func TestTeamKillCriteriaFollowTheTeams(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Killer"))
	killer := connect()
	killer.JoinAs("Killer")
	killer.MoveToGround(2.5, 64, 5.5, true)
	killer.Settle()
	srv.GiveSlot("Killer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 3))
	addObjective(t, killer, "tk", "teamkill.red")
	addObjective(t, killer, "kb", "killedByTeam.blue")
	addObjective(t, killer, "other", "teamkill.blue")
	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	cowUUID, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("summoned cow has no UUID")
	}
	for _, line := range []string{"team add red", "team modify red color red", "team add blue",
		"team modify blue color blue", "team join blue Killer", "team join red " + cowUUID} {
		srv.RunLine(line)
	}
	killer.Settle()
	servertest.AwaitFullSwing(t, srv, "Killer")
	killer.Attack(cow)
	waitScore(t, srv, "Killer", "tk", 1)
	waitScore(t, srv, cowUUID, "kb", 1)
	if v, ok := srv.ScoreOf("Killer", "other"); ok {
		t.Errorf("Killer has %d in teamkill.blue after killing a red cow, want no score", v)
	}
}

// TestHealthCriteriaCountsAbsorptionAndReachesTheBoardInTicks checks that health is
// ceil(health+absorption) and that a change reaches the board within a tick
// or two.
// vanilla: ServerPlayer.doTick
func TestHealthCriteriaCountsAbsorptionAndReachesTheBoardInTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	addObjective(t, c, "hp", "health")
	// Low food stops natural regeneration from lifting the 19 before it is read.
	srv.SetFood("Testificate", 7)
	srv.SetHealth("Testificate", 19)
	waitScore(t, srv, "Testificate", "hp", 19)

	c.ChatCommand("effect give Testificate absorption 100 0")
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	srv.SetHealth("Testificate", 10.5)
	servertest.RunTicks(t, srv, 3)
	if v, _ := srv.ScoreOf("Testificate", "hp"); v != 15 {
		t.Errorf("hp = %d after a tick with 10.5 health and 4 absorption, want 15", v)
	}
}

// TestKillCreditFollowsTheLastAttacker checks that a player who hurt another
// is credited with the kill when something else lands the final blow.
// vanilla: ServerPlayer.die, LivingEntity.getKillCredit
func TestKillCreditFollowsTheLastAttacker(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Killer", "Victim"))
	killer := connect()
	killer.JoinAs("Killer")
	victim := connect()
	victimID := victim.JoinAs("Victim")
	killer.MoveToGround(2.5, 64, 5.5, true)
	killer.MoveToGround(2.5, 64, 5.5, true)
	victim.MoveToGround(2.5, 64, 6.5, true)
	victim.Settle()
	killer.Settle()
	addObjective(t, killer, "total", "totalKillCount")
	addObjective(t, killer, "players", "playerKillCount")
	srv.GiveSlot("Killer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 3))

	servertest.AwaitFullSwing(t, srv, "Killer")
	killer.Attack(victimID)
	killer.Settle()
	victim.ChatCommand("damage Victim 1000 minecraft:magic")
	victim.Settle()
	waitScore(t, srv, "Killer", "players", 1)
	waitScore(t, srv, "Killer", "total", 1)
}
