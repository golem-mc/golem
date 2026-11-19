package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractFurnaceBlockEntity.loadAdditional, serverTick
func TestALoadedFurnaceUsesItsStoredCookTotal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, timers string
		ticks        int
	}{
		{"missing total", "", 2},
		{"lit zero total", "lit_time_remaining:100,", 2},
		{"lit stored total", "lit_time_remaining:100,cooking_total_time:5,", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
			servertest.HoldTicks(t, srv)
			servertest.Setblock(t, c, 2, -60, 3, `furnace[facing=north]{`+tc.timers+`Items:[{Slot:0b,id:"minecraft:iron_ore",count:8},{Slot:1b,id:"minecraft:coal",count:2}]}`)
			servertest.RunTicks(t, srv, tc.ticks)
			for _, field := range []struct{ path, want string }{
				{`Items[{Slot:2b}].id`, `"minecraft:iron_ingot"`},
				{`Items[{Slot:2b}].count`, "1"},
				{`Items[{Slot:0b}].count`, "7"},
				{`RecipesUsed."minecraft:iron_ingot_from_smelting_iron_ore"`, "1"},
				{"cooking_total_time", "200"},
			} {
				c.ChatCommand("data get block 2 -60 3 " + field.path)
				c.ExpectCommandFeedback("commands.data.block.query", "2", "-60", "3", field.want)
			}
			// Subsequent recipes use the normal duration, starting at the first smelt.
			servertest.RunTicks(t, srv, 200-tc.ticks)
			c.ChatCommand("data get block 2 -60 3 Items[{Slot:2b}].count")
			c.ExpectCommandFeedback("commands.data.block.query", "2", "-60", "3", "1")
			servertest.RunTicks(t, srv, tc.ticks)
			c.ChatCommand("data get block 2 -60 3 Items[{Slot:2b}].count")
			c.ExpectCommandFeedback("commands.data.block.query", "2", "-60", "3", "2")
		})
	}
}

// vanilla: AbstractFurnaceBlockEntity.setItem, serverTick
func TestAMenuFilledFurnaceTakes200Ticks(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, w := servertest.OpenWindowed(t, cfg)
	servertest.HoldTicks(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:iron_ore", 8, 2)
	servertest.RunTicks(t, srv, 199)
	if got := srv.InventoryAt(4, 64, 0)[2]; !got.Empty() {
		t.Fatalf("output after 199 ticks = %+v, want empty", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := srv.InventoryAt(4, 64, 0)[2]; !got.Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 1)) {
		t.Fatalf("output after 200 ticks = %+v, want one iron ingot", got)
	}
}

// vanilla: AbstractFurnaceBlockEntity.serverTick
func TestARelightingFurnaceRescalesStoredProgress(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 2, -60, 3, `furnace{cooking_total_time:400,cooking_time_spent:101,Items:[{Slot:0b,id:"minecraft:iron_ore",count:8},{Slot:1b,id:"minecraft:coal",count:2}]}`)
	servertest.RunTicks(t, srv, 1)
	c.ChatCommand("data get block 2 -60 3 cooking_time_spent")
	c.ExpectCommandFeedback("commands.data.block.query", "2", "-60", "3", "52")
	c.ChatCommand("data get block 2 -60 3 cooking_total_time")
	c.ExpectCommandFeedback("commands.data.block.query", "2", "-60", "3", "200")
}
