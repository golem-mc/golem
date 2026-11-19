package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestHopperCompostableComponentsSurviveRestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, client := servertest.OpenWindowedOps(t, cfg)
	servertest.HoldTicks(t, srv)
	// Save both patch kinds in locked hoppers before exercising their transfer.
	for i, component := range []string{`"minecraft:compostable":{layers:7}`, `"!minecraft:compostable":{}`} {
		x := int32(4 + i*4)
		srv.SetBlock(x, 63, 0, composterLevel(t, "0"))
		srv.SetBlock(x+1, 64, 0, registry.BlockRedstoneBlock.DefaultState())
		servertest.Setblock(t, client.Client, x, 64, 0, `minecraft:hopper[enabled=false]{Items:[{Slot:0b,id:"minecraft:apple",count:2,components:{`+component+`}}]}`)
	}
	client.Disconnect()
	srv.Shutdown()
	back, watcher := servertest.OpenWindowedOps(t, cfg)
	servertest.HoldTicks(t, back)
	for _, x := range []int32{4, 8} {
		slots := back.InventoryAt(x, 64, 0)
		if len(slots) != 5 || slots[0].Count != 2 {
			t.Fatalf("saved hopper at %d = %+v", x, slots)
		}
		back.SetBlock(x+1, 64, 0, 0)
		back.SetBlock(x, 64, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"}))
	}
	servertest.RunTicks(t, back, 1)
	if got := back.BlockAt(4, 63, 0); got != composterLevel(t, "7") {
		t.Fatalf("patched apple produced state %d, want level 7", got)
	}
	if got := back.InventoryAt(4, 64, 0)[0]; got.Count != 1 {
		t.Errorf("patched apples left = %d, want 1", got.Count)
	}
	if got := back.BlockAt(8, 63, 0); got != composterLevel(t, "0") {
		t.Errorf("removed component composted: state %d", got)
	}
	removed := back.InventoryAt(8, 64, 0)[0]
	if removed.Count != 2 || !removed.Removes(protocol.DataComponentCompostable) {
		t.Errorf("removed apples after reload = %+v", removed)
	}
	servertest.RunTicks(t, back, 19)
	if back.BlockAt(4, 63, 0) != composterLevel(t, "7") {
		t.Fatal("hopper compost ready before 20 ticks")
	}
	servertest.RunTicks(t, back, 1)
	if back.BlockAt(4, 63, 0) != composterLevel(t, "8") {
		t.Fatal("hopper compost not ready after 20 ticks")
	}
	watcher.Disconnect()
}
