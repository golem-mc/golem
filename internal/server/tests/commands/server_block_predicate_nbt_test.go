package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestBlockPredicateMatchesBlockEntityNBT checks a block predicate's
// compound: it picks only a cell whose block entity's saved tag contains it,
// in /execute if block, /clone filtered and /fill replace alike, and a cell
// with no block entity fails it even where the block or tag matches.
// vanilla: BlockPredicateArgument.BlockPredicate.test, TagPredicate.test
func TestBlockPredicateMatchesBlockEntityNBT(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Two chests two cells apart, so they never join; the first holds a
	// diamond.
	c.ChatCommand("setblock 2 100 2 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "2", "100", "2")
	c.ChatCommand("setblock 4 100 2 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "4", "100", "2")
	c.ChatCommand("item replace block 2 100 2 container.0 with minecraft:diamond")
	c.ExpectCommandFeedbackText(itemBlockSetKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(100), servertest.ItemNumber(2),
		servertest.ItemFeedbackText("minecraft:diamond", servertest.ItemDisplayKey(t, "minecraft:diamond"), 1))
	c.ChatCommand("setblock 6 100 2 minecraft:stone")
	c.ExpectCommandFeedback("commands.setblock.success", "6", "100", "2")

	pass := plainFeedbackTextsForTest("commands.execute.conditional.pass")
	fail := servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")
	const diamondChest = `minecraft:chest{Items:[{id:"minecraft:diamond"}]}`
	for _, tc := range []struct {
		command string
		want    []byte
	}{
		{"execute if block 2 100 2 " + diamondChest, pass},
		{"execute if block 4 100 2 " + diamondChest, fail},
		{`execute if block 2 100 2 #minecraft:guarded_by_piglins[type=single]{Items:[{id:"minecraft:diamond"}]}`, pass},
		{"execute if block 6 100 2 #minecraft:replaceable{}", fail},
		// Air is in the tag, but holds no block entity for {} to match.
		{"execute if block 3 100 2 #minecraft:replaceable", pass},
		{"execute if block 3 100 2 #minecraft:replaceable{}", fail},
	} {
		c.ChatCommand(tc.command)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, tc.want) {
			t.Errorf("/%s answered % X, want % X", tc.command, got, tc.want)
		}
	}

	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the registry has no chest")
	}

	one := int64(1)

	// The filtered clone copies the diamond chest alone.
	c.ChatCommand("clone 2 100 2 4 100 2 2 102 2 filtered " + diamondChest)
	c.ExpectCommandFeedbackText("commands.clone.success", protocol.Text{Number: &one})
	if got := srv.BlockAt(2, 102, 2); got != chest {
		t.Errorf("clone destination of the diamond chest = %d, want the chest %d", got, chest)
	}
	if got := srv.BlockAt(4, 102, 2); got != 0 {
		t.Errorf("clone destination of the empty chest = %d, want air", got)
	}

	// The fill clears the diamond chest alone.
	c.ChatCommand("fill 2 100 2 4 100 2 minecraft:air replace " + diamondChest)
	c.ExpectCommandFeedbackText("commands.fill.success", protocol.Text{Number: &one})
	if got := srv.BlockAt(2, 100, 2); got != 0 {
		t.Errorf("the diamond chest's cell = %d after the fill, want air", got)
	}
	if got := srv.BlockAt(4, 100, 2); got != chest {
		t.Errorf("the empty chest's cell = %d after the fill, want the chest %d", got, chest)
	}
	c.Disconnect()
}
