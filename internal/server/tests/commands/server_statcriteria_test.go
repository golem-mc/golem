package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestMinedStoneRaisesAStatisticObjective checks that a statistic criteria is
// accepted, that breaking stone writes the score of every objective that
// counts it and no other, and that the Update Score packet carries it.
// vanilla: ObjectiveCriteria.byName, ServerPlayer.awardStat
func TestMinedStoneRaisesAStatisticObjective(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalConfig(), "Miner", servertest.OpsListing(t, "Miner"))
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Miner", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5))
	addObjective(t, c, "stone", "minecraft.mined:minecraft.stone")
	addObjective(t, c, "dirt", "minecraft.mined:minecraft.dirt")
	c.ChatCommand("scoreboard objectives setdisplay sidebar stone")
	c.Settle()

	target := [3]int32{2, 64, 0}
	if !srv.SetBlock(target[0], target[1], target[2], registry.MustState("minecraft:stone", nil)) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()
	c.BreakBlock(target[0], target[1], target[2])
	servertest.UntilTicks(t, srv, 60, "the stone to break", func() bool {
		return srv.BlockAt(target[0], target[1], target[2]) == 0
	})
	waitScore(t, srv, "Miner", "stone", 1)
	if v, ok := srv.ScoreOf("Miner", "dirt"); ok && v != 0 {
		t.Errorf("the dirt objective = %d after mining stone, want none", v)
	}
}

// TestUnknownStatisticCriteriaAreRefused checks that a stat type or key
// that does not exist is an invalid criteria, and that a bare key is read in
// minecraft.
// vanilla: ObjectiveCriteria.byName
func TestUnknownStatisticCriteriaAreRefused(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")
	for _, bad := range []string{"minecraft.mined:minecraft.nonsense", "minecraft.nonsense:minecraft.stone", "minecraft.mined:", "mined"} {
		c.ChatCommand("scoreboard objectives add bad " + bad)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); string(got) != string(servertest.CommandErrorMessageForTest("argument.criteria.invalid", bad)) {
			t.Errorf("criteria %q answered % X, want argument.criteria.invalid", bad, got)
		}
		c.ExpectPlay(protocol.IDPlaySystemChat) // the context line
	}
	addObjective(t, c, "bare", "mined:stone")
}
