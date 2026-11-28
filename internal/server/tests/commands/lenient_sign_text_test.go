package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSetblockSignWithUnreadableHoverDropsHover sets a sign whose first line
// has an unreadable hover_event and verifies /data get block reads plain "a",
// while /give still refuses the same unreadable hover in item components.
// vanilla: SignText.DIRECT_CODEC, TagValueInput.read, ItemParser
func TestSetblockSignWithUnreadableHoverDropsHover(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, _ := servertest.Join(t, cfg, "Signer", servertest.OpsListing(t, "Signer"))

	srv.RunLine(`setblock 2 64 2 oak_sign{front_text:{messages:[{text:"a",hover_event:{action:"show_text"}},"b","c","d"]}}`)

	got := plainQuery(t, srv, `data get block 2 64 2 front_text.messages[0]`, "commands.data.block.query")
	if got != `"a"` {
		t.Errorf("front_text.messages[0] = %s, want %q", got, `"a"`)
	}

	// /give must still refuse an unreadable hover in custom_name, item_name, and lore.
	for _, cmd := range []string{
		`give Signer stone[minecraft:custom_name={text:"S",hover_event:{action:"show_text"}}]`,
		`give Signer stone[minecraft:item_name={text:"S",hover_event:{action:"show_text"}}]`,
		`give Signer stone[minecraft:lore=[{text:"S",hover_event:{action:"show_text"}}]]`,
	} {
		replies := srv.RunLine(cmd)
		if len(replies) == 0 || replies[0].Translate == "commands.give.success.single" {
			t.Errorf("%s succeeded (%+v), want refusal", cmd, replies)
		}
	}
}
