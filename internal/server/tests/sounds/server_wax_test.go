package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWaxingABlockSoundsToOthersNotTheWaxer: the wax-on level event is
// particles only, so a honeycomb on copper also plays item.honeycomb.wax_on
// to everyone but the player using it.
// vanilla: HoneycombItem.useOn
func TestWaxingABlockSoundsToOthersNotTheWaxer(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:honeycomb")
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:exposed_copper_trapdoor",
		"facing=south,half=bottom,open=true,powered=false,waterlogged=false"))
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	actor.Settle()
	actor.UseBlock(3, 64, 0)
	actor.Settle()

	const wax = "minecraft:item.honeycomb.wax_on"
	if got := servertest.HearTool(t, watcher).Sounds[wax]; got != 1 {
		t.Errorf("watcher heard %d wax sounds, want 1", got)
	}
	if got := servertest.HearTool(t, actor).Sounds[wax]; got != 0 {
		t.Errorf("the waxer heard %d wax sounds, want 0", got)
	}
}
