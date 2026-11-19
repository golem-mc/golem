package advancements

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: EntityItemAccessor.setItems, ServerPlayer.containerListener, PlayerAdvancements.award
func TestItemChangesAnnounceAdvancementsBeforeFeedback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, setup, command, title, feedback string
	}{
		{"iron chestplate", "", "item replace entity @s armor.chest with minecraft:iron_chestplate", "advancements.story.obtain_armor.title", "commands.item.entity.replace.success.single.known_item"},
		{"diamond helmet", "", "item replace entity @s armor.head with minecraft:diamond_helmet", "advancements.story.shiny_gear.title", "commands.item.entity.replace.success.single.known_item"},
		{"from storage", "item replace entity @s enderchest.0 with minecraft:iron_chestplate", "item replace entity @s armor.chest from entity @s enderchest.0", "advancements.story.obtain_armor.title", "commands.item.entity.replace.success.single"},
		{"fill", "", "item fill entity @s hotbar.* with minecraft:iron_chestplate", "advancements.story.obtain_armor.title", "commands.item.entity.replace.success.single.known_item"},
		{"override", "", "item override entity @s hotbar.* with minecraft:iron_chestplate", "advancements.story.obtain_armor.title", "commands.item.entity.replace.success.single.known_item"},
		{"modify", "item replace entity @s armor.chest with minecraft:leather_chestplate", `item modify entity @s armor.chest {type:"set_item",item:"iron_chestplate"}`, "advancements.story.obtain_armor.title", "commands.item.entity.modify.success.single"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Admin", servertest.OpsListing(t, "Admin"))
			c.WatchAdvancements = true
			servertest.HoldTicks(t, srv)
			c.Settle()
			if tc.setup != "" {
				c.ChatCommand(tc.setup)
				c.Settle()
			}

			c.ChatCommand(tc.command)
			var order []string
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySystemChat {
					return false
				}
				tag, _, err := nbt.DecodeNetwork(data)
				if err != nil {
					t.Fatal(err)
				}
				message, _ := tag.(nbt.Compound)
				translation, _ := nbt.Get[nbt.String](message, "translate")
				if strings.HasPrefix(string(translation), "chat.type.advancement.") {
					text, ok := protocol.ReadText(protocol.NewReader(data))
					if !ok {
						t.Fatal("advancement announcement does not decode")
					}
					if len(text.With) != 2 || text.With[0].Text != "Admin" || text.With[1].Translate != "chat.square_brackets" ||
						len(text.With[1].With) != 1 || text.With[1].With[0].Translate != tc.title {
						t.Errorf("announcement = %+v, want Admin earning %s", text, tc.title)
					}
					order = append(order, "announcement")
				} else {
					order = append(order, string(translation))
				}
				return false
			})
			if want := []string{"announcement", tc.feedback}; !slices.Equal(order, want) {
				t.Errorf("chat order = %v, want %v", order, want)
			}
		})
	}
}
