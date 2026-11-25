package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestLodestoneTickKeepsReplacementAfterDimensionChange checks answers read for
// the world the player left do not clear a stack that replaced the old one.
func TestLodestoneTickKeepsReplacementAfterDimensionChange(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpawning(false)
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { s.Shutdown(); slog.SetDefault(logger) })

	const x, z = 20000, 20000
	s.SetLoadedBlock(x, 62, z, registry.BlockStone.DefaultState())
	tracked := func(dim string) protocol.ItemStack {
		st := protocol.ItemStack{ItemID: int32(registry.ItemCompass), Count: 1}
		st.SetLodestoneTracker(protocol.LodestoneTracker{
			Target:  &protocol.LodestoneTarget{Dimension: dim, X: x, Y: 63, Z: z},
			Tracked: true,
		})
		return st
	}
	c := &client{srv: s, player: &player.Player{EntityID: s.nextEntity()}}
	c.dim.Store(s.allDims()[0])
	slot := player.MainSlot
	c.player.Inventory(func(inv *player.Inventory) { inv.Set(slot, tracked("minecraft:overworld")) })
	c.compassRead = func() {
		c.dim.Store(s.allDims()[1])
		c.player.Inventory(func(inv *player.Inventory) { inv.Set(slot, tracked("minecraft:overworld")) })
	}
	c.tickLodestoneCompasses()
	c.player.Inventory(func(inv *player.Inventory) {
		tr, ok := inv.Slots[slot].LodestoneTrackerOf()
		if !ok || tr.Target == nil {
			t.Errorf("the replacement lost its target after the dimension change: %+v", tr)
		}
	})
}
