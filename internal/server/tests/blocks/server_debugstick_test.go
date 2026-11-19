package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// debugStickScene joins the actor, with the debug stick in hand, beside the
// block at (4, 64, 5) and puts blockName there.
func debugStickScene(t *testing.T, blockName string, setup ...func(*server.Server)) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), setup...)
	watcher := connect().Join("Watcher")
	actor := connect().Join("Actor")
	stick, _ := registry.ItemID("minecraft:debug_stick")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stick, Count: 1})
	servertest.PrepareGameMasterPlacement(actor)
	state, _ := registry.FindState(blockName, nil)
	srv.SetBlock(4, 64, 5, state)
	actor.Settle()
	watcher.Settle()
	return srv, actor, watcher
}

// expectDebugStickLine reads the next action bar line and checks it.
func expectDebugStickLine(t *testing.T, c *servertest.Client, key string, args ...string) {
	t.Helper()
	with := make([]protocol.Text, len(args))
	for i, a := range args {
		with[i] = protocol.Text{Text: a}
	}
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key, With: with})
	want.PutBool(true)
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySystemChat)
	if !ok {
		t.Fatalf("no %s line within %s", key, servertest.ExpectTimeout)
	}
	if string(data) != string(want.Bytes()) {
		t.Errorf("the action bar line = % X, want % X", data, want.Bytes())
	}
}

// stairFacing is the facing the stair at (4, 64, 5) has.
func stairFacing(t *testing.T, srv *server.Server) string {
	t.Helper()
	_, props, _ := registry.BlockStateName(srv.BlockAt(4, 64, 5))
	return registry.ParseStateProps(props)["facing"]
}

// TestADebugStickSelectsThenCyclesAProperty is the stick's whole loop for a
// creative operator: a left click selects the stair's first property and keeps
// the block, a use steps its value and tells the watcher.
// vanilla: DebugStickItem.handleInteraction
func TestADebugStickSelectsThenCyclesAProperty(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := debugStickScene(t, "minecraft:oak_stairs", servertest.OpsListing(t, "Actor"))
	order, _ := registry.BlockPropertyOrder("minecraft:oak_stairs", "facing")

	actor.StartDigging(4, 64, 5)
	expectDebugStickLine(t, actor, "item.minecraft.debug_stick.select", "facing", "north")
	actor.Settle()
	if got := stairFacing(t, srv); got != "north" {
		t.Fatalf("the stair after a left click faces %q, want it kept (north)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot).DebugStick["minecraft:oak_stairs"]; got != "facing" {
		t.Errorf("the stick's selection for stairs = %q, want facing", got)
	}

	actor.UseBlock(4, 64, 5)
	expectDebugStickLine(t, actor, "item.minecraft.debug_stick.update", "facing", order[1])
	if got := stairFacing(t, srv); got != order[1] {
		t.Errorf("the stair after a use faces %q, want %q", got, order[1])
	}
	if got := watcher.ExpectBlockUpdate(); got.X != 4 || got.Y != 64 || got.Z != 5 {
		t.Errorf("the watcher's block update at (%d,%d,%d), want (4,64,5)", got.X, got.Y, got.Z)
	}
}

// TestADebugStickDoesNothingForAPlayerWithoutPermission shows no line and
// leaves the block and the stick as they were; the left click breaks nothing.
// vanilla: Player.canUseGameMasterBlocks
func TestADebugStickDoesNothingForAPlayerWithoutPermission(t *testing.T) {
	t.Parallel()
	srv, actor, _ := debugStickScene(t, "minecraft:oak_stairs")
	before := srv.BlockAt(4, 64, 5)

	actor.StartDigging(4, 64, 5)
	actor.UseBlock(4, 64, 5)
	actor.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySystemChat {
			t.Error("a player without permission got an action bar line")
		}
	})
	if got := srv.BlockAt(4, 64, 5); got != before {
		t.Errorf("the block after a refused stick = %d, want %d", got, before)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot).DebugStick; len(got) != 0 {
		t.Errorf("the stick's selection = %v, want none", got)
	}
}
