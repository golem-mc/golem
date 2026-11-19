package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func boldText(s string) protocol.Text {
	return protocol.Text{Raw: nbt.Compound{"text": nbt.String(s), "bold": nbt.Byte(1)}}
}

// TestStyledObjectiveAndTeamNamesKeepTheirStyleInFeedback checks that a
// display name with a style the Text model has no field for is relayed whole.
// vanilla: Objective.getFormattedDisplayName, PlayerTeam.getFormattedDisplayName
func TestStyledObjectiveAndTeamNamesKeepTheirStyleInFeedback(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Styler", servertest.OpsListing(t, "Styler"))
	brackets := func(s string) protocol.Text {
		return protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{boldText(s)}}
	}
	c.ChatCommand(`scoreboard objectives add o dummy {text:"C",bold:true}`)
	c.ExpectComponentFeedback("commands.scoreboard.objectives.add.success", brackets("C"))
	c.ChatCommand(`team add ta {text:"TA",bold:true}`)
	c.ExpectPlay(protocol.IDPlaySetPlayerTeam)
	c.ExpectPlay(protocol.IDPlaySetPlayerTeam)
	c.ExpectComponentFeedback("commands.team.add.success", brackets("TA"))
}

// TestSummonedNameKeepsItsBoldInFeedback checks the success line names the
// entity by its stored custom name, bold included.
// vanilla: Entity.getDisplayName
func TestSummonedNameKeepsItsBoldInFeedback(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Styler", servertest.OpsListing(t, "Styler"))
	c.ChatCommand(`summon husk ~ ~ ~ {CustomName:{text:"B",bold:true}}`)
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !containsBytes(data, []byte("bold")) {
		t.Errorf("summon feedback lost the bold style: % X", data)
	}
}

func containsBytes(data, sub []byte) bool { return bytes.Contains(data, sub) }
