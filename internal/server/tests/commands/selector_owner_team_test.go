package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntitySelectorOptions team option, TamableAnimal.getTeam
func TestSelectorTeamInheritsTameRootOwner(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	words := servertest.OfflineUUIDWords("Alice")
	// Decimal int-array elements are signed, including UUID words with the high bit set.
	// vanilla: SnbtGrammar.IntegerLiteral.create
	owner := fmt.Sprintf("[I;%d,%d,%d,%d]", int32(words[0]), int32(words[1]), int32(words[2]), int32(words[3]))
	run := func(line, key string) {
		t.Helper()
		if got := srv.RunLine(line); len(got) != 1 || got[0].Translate != key {
			t.Fatalf("%s: feedback = %+v, want %s", line, got, key)
		}
	}
	srv.RunLine("team add owners")
	srv.RunLine("team add pets")
	srv.RunLine("team join owners Alice")

	check := func(tag, option string, pass bool) {
		t.Helper()
		c.Drain()
		c.ChatCommand("execute if entity @e[tag=" + tag + ",team=" + option + "]")
		if pass {
			c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: new(int64(1))})
		} else {
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
		}
	}
	for _, kind := range []string{"wolf", "cat", "parrot", "nautilus"} {
		t.Log(kind)
		run(fmt.Sprintf(`summon %s 4 64 4 {Owner:%s,NoAI:1b,Tags:["%s"]}`, kind, owner, kind), "commands.summon.success")
		check(kind, "owners", true)
		check(kind, "!owners", false)
		check(kind, "", false)
		check(kind, "!", true)
		check(kind, "!pets,team=!", true)

		srv.RunLine("team join pets @e[tag=" + kind + "]")
		check(kind, "pets", true)
		check(kind, "owners", false)
		check(kind, "!owners", true)
		check(kind, "!pets", false)
		srv.RunLine("team leave @e[tag=" + kind + "]")
		check(kind, "owners", true)
	}

	// No tick passes between commands: membership must be resolved live.
	srv.RunLine("team join pets Alice")
	check("wolf", "pets", true)
	check("wolf", "owners", false)
	srv.RunLine("team leave Alice")
	check("wolf", "", true)
	check("wolf", "!", false)
	srv.RunLine("team join owners Alice")
	check("wolf", "owners", true)

	// An offline scoreboard holder is not an owner entity.
	srv.RunLine("team join owners Offline")
	words = servertest.OfflineUUIDWords("Offline")
	run(fmt.Sprintf(`data merge entity @e[tag=wolf,limit=1] {Owner:[I;%d,%d,%d,%d]}`, int32(words[0]), int32(words[1]), int32(words[2]), int32(words[3])), "commands.data.entity.modified")
	check("wolf", "owners", false)
	check("wolf", "", true)

	// The root owner's team wins over an intermediate owner's own team.
	parent := "[I;1,2,3,4]"
	run(fmt.Sprintf(`summon cat 5 64 5 {UUID:[I;1,2,3,4],Owner:%s,Team:"pets",NoAI:1b}`, owner), "commands.summon.success")
	run(fmt.Sprintf(`data merge entity @e[tag=wolf,limit=1] {Owner:%s}`, parent), "commands.data.entity.modified")
	check("wolf", "owners", true)
	check("wolf", "pets", false)

	run(`summon wolf 6 64 6 {NoAI:1b,Tags:["wild"]}`, "commands.summon.success")
	check("wild", "", true)
	check("wild", "owners", false)
	c.Disconnect()
}
