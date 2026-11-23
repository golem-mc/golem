package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DataComponents.ENCHANTMENT_GLINT_OVERRIDE is Codec.BOOL, saved as 0b/1b.
func TestGlintOverrideSavesAsBooleanBytes(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	const path = `Items[0].components."minecraft:enchantment_glint_override"`
	c.ChatCommand("setblock 2 64 3 minecraft:chest")
	servertest.DrainBlockUpdates(t, c, 8)

	for _, tc := range []struct {
		value, saved string
		want         int8
	}{
		{"false", "0b", protocol.GlintHidden},
		{"true", "1b", protocol.GlintShown},
	} {
		c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:diamond_sword[enchantment_glint_override=" + tc.value + "]")
		c.Settle()
		c.ChatCommand("data get block 2 64 3 " + path)
		c.ExpectCommandFeedback("commands.data.block.query", "2", "64", "3", tc.saved)

		// The stored stack round-trips into the player's slot with its glint.
		c.ChatCommand("item replace entity @s hotbar.6 from block 2 64 3 container.0")
		updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
		if got := updates[0].Stack; got.ItemID != servertest.MustItem("minecraft:diamond_sword") || got.GlintOverride != tc.want {
			t.Errorf("glint %s: copied stack = %+v, want a sword with glint %d", tc.value, got, tc.want)
		}
	}
}
