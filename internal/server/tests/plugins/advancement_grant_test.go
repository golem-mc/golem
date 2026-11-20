package plugins_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: PlayerAdvancements.award
func TestPluginCriterionGrantCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{true, false} {
		name := "granted"
		if cancel {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			var seen []string
			srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "grant", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerAdvancementCriterionGrantEvent) {
					if e.Player.Name() != "Picker" {
						return
					}
					seen = append(seen, e.Advancement.Key().String()+"/"+e.Criterion)
					e.SetCancelled(cancel && e.Criterion != "unlock_right_away")
				})
				return nil
			}}}})
			c := servertest.DialServer(t, srv)
			c.WatchAdvancements = true
			c.Join("Picker")
			servertest.HoldTicks(t, srv)
			srv.GiveSlot("Picker", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:crafting_table"), Count: 1})
			srv.GiveSlot("Picker", player.HotbarSlot+1, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:oak_log"), Count: 1})
			var rootProgress int
			c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlayUpdateAdvancements:
					if bytes.Contains(data, []byte("minecraft:story/root")) && bytes.Contains(data, []byte("crafting_table")) {
						rootProgress++
					}
				}
				return false
			})
			got := srv.AdvancementObtained("Picker", "minecraft:story/root", "crafting_table")
			if got == cancel {
				t.Errorf("story/root crafting_table obtained = %v with cancel = %v", got, cancel)
			}
			if !srv.AdvancementObtained("Picker", "minecraft:recipes/decorations/crafting_table", "unlock_right_away") {
				t.Error("the unlock_right_away recipe advancement was not granted")
			}
			if len(seen) == 0 {
				t.Error("listener never fired")
			}
			if (rootProgress > 0) == cancel {
				t.Errorf("%d updates carried story/root's progress with cancel = %v", rootProgress, cancel)
			}
		})
	}
}
