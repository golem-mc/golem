package blocks

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// A placed block entity keeps the components of its stack that no implicit
// reader consumed, and a save and reload keeps them.
// vanilla: BlockEntity.applyComponents, BlockEntity.loadWithComponents
func TestPlacedBlockEntityKeepsLeftoverComponents(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, w := servertest.OpenWindowedOps(t, cfg)

	name := protocol.Text{Text: "Named"}
	barrel := servertest.ItemStack(t, "minecraft:barrel", 1)
	barrel.CustomName = &name
	barrel.SetLore([]protocol.Text{{Text: "Lore line"}})
	barrel.SetCustomData(nbt.Compound{"k": nbt.Byte(1)})
	w.SetCreativeSlot(36, barrel)
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	w.ChatCommand(`setblock 8 64 0 chest{components:{"minecraft:lore":["L2"],"minecraft:custom_data":{z:2b}}}`)
	servertest.AwaitSystemChatSaying(t, w.Client, "commands.setblock.success")

	read := func(s *world.World, x int32, wantData string, wantLore string) {
		t.Helper()
		tag, ok := s.BlockEntityTag(x, 64, 0)
		if !ok {
			t.Fatalf("no block entity at %d", x)
		}
		comps, _ := nbt.Get[nbt.Compound](tag, "components")
		if _, ok := comps["minecraft:lore"]; !ok {
			t.Errorf("x=%d components %v lack minecraft:lore (%s)", x, comps, wantLore)
		}
		data, _ := nbt.Get[nbt.Compound](comps, "minecraft:custom_data")
		if _, ok := data[wantData]; !ok {
			t.Errorf("x=%d custom_data %v lacks %s", x, data, wantData)
		}
		if _, ok := comps["minecraft:custom_name"]; ok {
			t.Errorf("x=%d components keep the consumed custom_name: %v", x, comps)
		}
	}
	live := srv.OverworldWorld()
	servertest.HoldTicks(t, srv)
	for _, pos := range []string{"4 64 0", "8 64 0"} {
		replies := srv.RunLine("data get block " + pos + " components")
		if len(replies) != 1 || replies[0].Translate != "commands.data.block.query" || len(replies[0].With) != 4 {
			t.Fatalf("data get %s replies = %+v", pos, replies)
		}
		if got := replies[0].With[3].Plain(); !strings.Contains(got, "minecraft:lore") || !strings.Contains(got, "minecraft:custom_data") || strings.Contains(got, "custom_name") {
			t.Errorf("/data get block %s components = %s", pos, got)
		}
	}
	read(live, 4, "k", "Lore line")
	read(live, 8, "z", "L2")

	w.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, _ := servertest.Join(t, &restarted, "Testificate")
	reloaded := back.OverworldWorld()
	read(reloaded, 4, "k", "Lore line")
	read(reloaded, 8, "z", "L2")
}

// A creative pick-block of a block entity carries its own components onto the
// picked stack, with the ones the kind consumed as the kind's.
// vanilla: ServerGamePacketListenerImpl.handlePickItemFromBlock, BlockEntity.collectComponents
func TestPickBlockCarriesTheBlockEntitysLeftoverComponents(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Picker", servertest.OpsListing(t, "Picker"))
	servertest.Setblock(t, c, 3, 64, 0, `chest{components:{"minecraft:lore":["L2"],"minecraft:custom_data":{z:2b}}}`)
	c.Settle()
	picked := servertest.PickedStack(t, c, 3, 64, 0, true)
	if len(picked.LoreOf()) != 1 {
		t.Errorf("picked lore = %v, want one line", picked.LoreOf())
	}
	if data := picked.CustomData(); data == nil || data["z"] == nil {
		t.Errorf("picked custom_data = %v, want z:2b", data)
	}
}
