package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// postEffectsPayload is the Post Effects packet for ids.
func postEffectsPayload(ids ...string) string {
	var b protocol.Buffer
	b.PutVarInt(int32(len(ids)))
	for _, id := range ids {
		b.PutString(id)
	}
	return string(b.Bytes())
}

func TestPostEffectCommandSendsTheSet(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	run := func(command string) (sets []string, chat int) {
		c.T.Helper()
		c.ChatCommand(command)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayPostEffects:
				sets = append(sets, string(data))
			case protocol.IDPlaySystemChat:
				chat++
			}
		})
		return sets, chat
	}
	step := func(command string, want ...string) {
		t.Helper()
		sets, chat := run(command)
		if len(sets) != 1 || sets[0] != postEffectsPayload(want...) {
			t.Errorf("%s: post effects packets = %x, want one of %q", command, sets, want)
		}
		if chat != 1 {
			t.Errorf("%s: %d feedback lines, want 1", command, chat)
		}
	}
	silent := func(command string) {
		t.Helper()
		if sets, chat := run(command); len(sets) != 0 || chat != 1 {
			t.Errorf("%s: sent %d sets and %d lines, want 0 and 1 (a failure)", command, len(sets), chat)
		}
	}

	step("posteffect add @s minecraft:blur", "minecraft:blur")
	silent("posteffect add @s blur")
	step("posteffect add @s my:glow", "minecraft:blur", "my:glow")
	var lines []string
	c.ChatCommand("posteffect list @s")
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayPostEffects:
			t.Errorf("list sent a post effects packet")
		case protocol.IDPlaySystemChat:
			lines = append(lines, string(data))
		}
	})
	if len(lines) != 1 || !strings.Contains(lines[0], "commands.posteffect.list.success") ||
		!strings.Contains(lines[0], "minecraft:blur, my:glow") {
		t.Errorf("list feedback = %q", lines)
	}
	step("posteffect remove @s blur", "my:glow")
	silent("posteffect remove @s blur")
	step("posteffect clear @s")
	silent("posteffect clear @s")
}

func TestPostEffectFeedbackNamesTheIdentifier(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()
	for _, verb := range []string{"add", "remove"} {
		c.ChatCommand("posteffect " + verb + " @s minecraft:creeper")
		var got protocol.Text
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlaySystemChat {
				got, _ = protocol.ReadText(protocol.NewReader(data))
			}
		})
		if got.Translate != "commands.posteffect."+verb+".success.single" || len(got.With) != 2 ||
			got.With[0].Text != "minecraft:creeper" || got.With[0].Translate != "" {
			t.Errorf("%s feedback = %+v, want the plain identifier as first argument", verb, got)
		}
	}
}
