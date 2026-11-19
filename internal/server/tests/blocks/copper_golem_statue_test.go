package blocks

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestClickingACopperGolemStatueCyclesItsPose: an item that is not an axe steps the
// pose and plays the statue sound. vanilla: CopperGolemStatueBlock.useItemOn
func TestClickingACopperGolemStatueCyclesItsPose(t *testing.T) {
	t.Parallel()
	srv, actor, watcher, events := statueScene(t)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:copper_golem_statue"])
	pose := func() string { return registry.StateValue(srv.BlockAt(3, 64, 0), registry.PropCopperGolemPose) }
	if got := pose(); got != "standing" {
		t.Fatalf("placed pose = %q, want standing", got)
	}

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := pose(); got != "sitting" {
		t.Errorf("pose after an empty hand = %q, want sitting", got)
	}
	if h := servertest.HearTool(t, watcher); h.Sounds["minecraft:entity.copper_golem_become_statue"] != 1 {
		t.Errorf("the watcher heard %v, want one become_statue", h.Sounds)
	}
	if got := events(); got != 1 {
		t.Errorf("block_change events at the statue = %d, want 1", got)
	}

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stick"), Count: 1})
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if got := pose(); got != "running" {
		t.Errorf("pose after a stick = %q, want running", got)
	}
}

// statueScene is a survival actor and a watcher at spawn in the smallest view,
// with the loop stopped, and a count of the block_change events at the statue
// cell (3, 64, 0).
func statueScene(t *testing.T) (*server.Server, *servertest.Client, *servertest.Client, func() int) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	watcher := connect().Join("Watcher")
	actor := connect().Join("Actor")
	watcher.ExpectPlayerAdded("Actor")
	srv.StopTick()
	actor.Settle()
	watcher.Settle()
	var mu sync.Mutex
	n := 0
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:block_change" && x == 3.5 && y == 64.5 && z == 0.5 {
			mu.Lock()
			n++
			mu.Unlock()
		}
	})
	return srv, actor, watcher, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// TestABrokenCopperGolemStatueDropsItsPose: the statue's item keeps the pose
// through copy_state. vanilla: CopyBlockState.run
func TestABrokenCopperGolemStatueDropsItsPose(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Sculptor")

	const x, y, z = int32(2), int32(64), int32(2)
	state, ok := registry.WithProp(registry.BlockStates["minecraft:copper_golem_statue"], registry.PropCopperGolemPose, "sitting")
	if !ok {
		t.Fatal("the statue has no sitting pose")
	}
	srv.SetBlock(x, y, z, state)
	srv.GiveSlot("Sculptor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1))
	c.MoveStep(0.5, 64, 0.5, true)
	c.Settle()

	c.BreakBlock(x, y, z)
	_, drop := c.WaitBreakSettled()
	if props, ok := drop.Stack.BlockStatePropertiesOf(); !ok || props["copper_golem_pose"] != "sitting" {
		t.Errorf("the dropped statue's block state = %v (ok %v), want copper_golem_pose sitting", props, ok)
	}
}
