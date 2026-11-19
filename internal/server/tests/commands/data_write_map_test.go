package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockDataAccessor.setData, MapDecorations.CODEC
func TestDataWriteKeepsMapDecorations(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c := servertest.Join(t, cfg, "Mapper", servertest.OpsListing(t, "Mapper"), freeze)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`setblock 3 64 3 chest`)
	servertest.AwaitSystemChatSaying(t, c, "commands.setblock.success")
	c.ChatCommand(`item replace block 3 64 3 container.0 with filled_map[map_id=42,map_decorations={"+":{type:"minecraft:monument",x:-120d,z:450d,rotation:180f}}]`)
	servertest.AwaitSystemChatSaying(t, c, itemBlockSetKey)
	want := protocol.ItemStack{ItemID: int32(registry.ItemFilledMap), Count: 1,
		MapDecorations: map[string]protocol.MapDecoration{"+": {Type: "minecraft:monument", X: -120, Z: 450, Rotation: 180}}}
	want.SetMapID(42)
	check := func(s *server.Server, stage string) {
		t.Helper()
		// Stored fallback NBT can retain a stack that the usable slot lost.
		slots := s.InventoryAt(3, 64, 3)
		if len(slots) == 0 {
			t.Fatalf("%s: chest inventory missing", stage)
		}
		if !slots[0].Equal(want) {
			t.Fatalf("%s: chest slot 0 = %+v, want decorated map", stage, slots[0])
		}
	}
	check(srv, "before data write")
	c.ChatCommand(`data merge block 3 64 3 {CustomName:"Maps"}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.block.modified")
	check(srv, "after unrelated data write")
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again := servertest.Join(t, &restarted, "Mapper", servertest.OpsListing(t, "Mapper"), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	check(back, "after region reload")
}
