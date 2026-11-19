package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBundleTemplateRemovalOrder gives a bundle whose template removes two
// components and reads the nested root back; the removals keep the order they
// were written in, not the table order of the server's removable components.
// vanilla: ItemStackTemplate.STREAM_CODEC, DataComponentPatch.CODEC
func TestBundleTemplateRemovalOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`give @s bundle[bundle_contents=[{id:"minecraft:diamond_pickaxe",count:1,components:{"!minecraft:food":{},"!minecraft:tool":{}}}]]`)
	servertest.AwaitSystemChatSaying(t, c, "commands.give.success.single")
	c.Settle()
	c.ChatCommand(`data get entity @s Inventory[{id:"minecraft:bundle"}].components."minecraft:bundle_contents"[0].components`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.With) < 2 {
		t.Fatalf("feedback %s", line.Plain())
	}
	got := line.With[len(line.With)-1].Plain()
	if !strings.Contains(got, `"!minecraft:food": {}, "!minecraft:tool": {}`) {
		t.Errorf("nested template root %s, want !food before !tool", got)
	}
}
