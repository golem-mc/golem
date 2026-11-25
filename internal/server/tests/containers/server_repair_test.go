package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: GrindstoneMenu.mergeItems, AnvilMenu.createResult
func TestRepairPairWithDifferentDurabilities(t *testing.T) {
	t.Parallel()
	pickaxe := func(durability, damage int32) protocol.ItemStack {
		stack := servertest.ItemStack(t, "minecraft:iron_pickaxe", 1)
		stack.SetDamage(damage)
		if durability != 250 {
			var data protocol.Buffer
			data.PutVarInt(durability)
			stack.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: data.Bytes()})
		}
		return stack
	}
	long := pickaxe(200, 180)
	short := pickaxe(100, 90)
	standard := pickaxe(250, 225)
	for _, tc := range []struct {
		name          string
		anvil         bool
		first, second protocol.ItemStack
		want          protocol.ItemStack
	}{
		{"grindstone/200_first", false, long, short, pickaxe(200, 160)},
		{"grindstone/100_first", false, short, long, pickaxe(200, 160)},
		{"grindstone/default_maximum", false, long, standard, pickaxe(250, 193)},
		{"anvil/200_first", true, long, short, pickaxe(200, 146)},
		{"anvil/100_first", true, short, long, pickaxe(100, 58)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, w := servertest.OpenWindowed(t, cfg)
			want := tc.want
			if tc.anvil {
				w.OpenAnvil(srv)
				srv.SetExperienceLevels("Testificate", 20)
				want.SetRepairCost(1)
			} else {
				openGrindstoneSurvival(t, srv, w)
			}

			srv.GiveSlot("Testificate", player.HotbarSlot, tc.first)
			srv.GiveSlot("Testificate", player.HotbarSlot+1, tc.second)
			w.Drain()
			w.Click(grindHotbar0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0: {}}, tc.first)
			w.Click(0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: tc.first}, protocol.ItemStack{})
			w.Click(grindHotbar0+1, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0 + 1: {}}, tc.second)
			w.Click(1, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{1: tc.second, 2: want}, protocol.ItemStack{})
			if tc.anvil && w.Props[server.AnvilCostProperty] != 2 {
				t.Errorf("anvil cost = %d, want 2", w.Props[server.AnvilCostProperty])
			}

			w.Click(2, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, want)
			w.Click(grindHotbar0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0: want}, protocol.ItemStack{})
			if tc.anvil && srv.ExperienceLevels("Testificate") != 18 {
				t.Errorf("levels after repair = %d, want 18", srv.ExperienceLevels("Testificate"))
			}
		})
	}
}
