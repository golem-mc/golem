package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// The @s selector, whose answer is the runner and nobody else: the vanilla
// EntitySelector's own branch, taken ahead of the sweep the other letters
// run, the source's own body tested against whatever filters the brackets
// gave and answered with nothing where it does not satisfy them. The tests
// stand a cow at the runner's feet and a creeper two blocks out — the bodies
// a @s resolved by sweeping could reach — and check the filtered self-kill
// refuses while the bare one takes the runner alone.
func TestSelfSelectorFiltersNameTheRunnerAlone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /kill is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, c, playerEntityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The two bodies the sweep would answer with: the cow at the very
	// cell the runner stands in, the creeper close enough for any
	// distance filter that takes them.
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 0.5)
	creeperID := srv.SpawnMob("minecraft:creeper", 0.5, 64, 3.5)
	if cowID == 0 || creeperID == 0 {
		t.Fatal("the bodies never stood up")
	}
	c.Settle()
	alive := srv.MobCount()

	// A type the runner is not: the vanilla @s answers nothing, the
	// command stops with the not-found refusal, and neither body dies to
	// it.
	for _, cmd := range []string{"kill @s[type=minecraft:creeper]", "kill @s[type=minecraft:cow]"} {
		c.ChatCommand(cmd)
		want := servertest.CommandErrorMessageForTest("argument.entity.notfound.entity")
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("/%s answered % X, want the not-found refusal % X", cmd, got, want)
		}
	}
	c.Settle()
	if got := srv.MobCount(); got != alive {
		t.Errorf("the world keeps %d mobs after the refused self-kills, want the %d it held", got, alive)
	}

	// The bare @s is the runner: the death is theirs, and the cow at
	// their feet stays where it was.
	c.ChatCommand("kill @s")
	data := c.AwaitPacket(protocol.IDPlayCombatDeath)
	if got := protocol.NewReader(data).ReadVarInt(); got != playerEntityID {
		t.Errorf("the death fell on entity %d, want the runner's %d", got, playerEntityID)
	}
	if got := srv.MobCount(); got != alive {
		t.Errorf("the world keeps %d mobs after the self-kill, want the %d it held — the cow did not die with them", got, alive)
	}
	c.Disconnect()
}
