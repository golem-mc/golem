package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestPackBlockTagsLoadAtStartAndReload checks that a data pack's block tag
// is what a block predicate's tag form tests, from startup and again after
// /reload rewrites the tag file.
// vanilla: TagLoader, ReloadableServerResources
func TestPackBlockTagsLoadAtStartAndReload(t *testing.T) {
	t.Parallel()
	cfg := packWorld(t)
	file := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "tags", "block", "reload_probe.json")
	writeFile(t, file, `{"values":["minecraft:stone","#minecraft:dirt",{"id":"nope:missing","required":false}]}`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 0 80 0 minecraft:stone")
	servertest.FeedbackComponent(t, c)
	expectTagTest(t, c, true)

	writeFile(t, file, `{"values":["minecraft:dirt"]}`)
	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	expectTagTest(t, c, false)
}

func expectTagTest(t *testing.T, c *servertest.Client, pass bool) {
	t.Helper()
	if pass {
		c.ChatCommand("execute if block 0 80 0 #pk:reload_probe run say hi")
		if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
			t.Errorf("the tag test said % X, want the tail's say", got)
		}
		return
	}
	c.ChatCommand("execute if block 0 80 0 #pk:reload_probe")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the tag test answered % X, want the conditional failure", got)
	}
}
