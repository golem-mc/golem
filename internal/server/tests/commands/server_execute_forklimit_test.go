package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestExecuteStopsAtTheForkLimit checks max_command_forks: a step whose
// contexts come to the limit stops the whole chain before its run, silently
// where the step ran in forked mode and with the limit's refusal where it did
// not; a chain that stays under it runs every context, and the conditional a
// chain ends on is its command, which the limit does not hold.
// vanilla: BuildContexts.execute
func TestExecuteStopsAtTheForkLimit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")
	for i := range 3 {
		srv.SetMobNoAI(srv.SpawnMob("minecraft:cow", 4.5+2*float64(i), 64, 4.5))
	}
	c.Settle()
	const chain = "execute as @e[type=minecraft:cow] as @e[type=minecraft:cow] run say hi"

	// Three cows by three cows is nine contexts, one under ten.
	srv.SetGameRuleValue("max_command_forks", 10)
	c.ChatCommand(chain)
	for range 9 {
		c.ExpectPlay(protocol.IDPlayDisguisedChat)
	}
	expectNoChat(t, c)

	// At nine, the second as comes to the limit at its third context: no
	// say, and the forked chain's refusal is unheard.
	srv.SetGameRuleValue("max_command_forks", 9)
	c.ChatCommand(chain)
	expectNoChat(t, c)
	// The as over all three cows already makes three.
	srv.SetGameRuleValue("max_command_forks", 3)
	c.ChatCommand("execute as @e[type=minecraft:cow] run say hi")
	expectNoChat(t, c)

	// At one, a positioned over the runner's one context comes to it
	// outside forked mode, which the runner hears.
	srv.SetGameRuleValue("max_command_forks", 1)
	c.ChatCommand("execute positioned ~ ~ ~ run say hi")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, commandErrorNumbersForTest("command.forkLimit", 1)) {
		t.Errorf("positioned at the limit answered % X, want the fork limit's refusal", got)
	}
	expectNoChat(t, c)
	// An as is a fork: the same limit stops it unheard.
	c.ChatCommand("execute as @s run say hi")
	expectNoChat(t, c)
	// A chain that ends on its conditional runs it, the limit
	// notwithstanding.
	c.ChatCommand("execute if entity @s")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("a chain ending on its conditional answered % X, want the pass", got)
	}
	c.Disconnect()
}
