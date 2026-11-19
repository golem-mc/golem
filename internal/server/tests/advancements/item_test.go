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

// vanilla: AbstractContainerMenu.triggerSlotListeners, ServerPlayer.containerListener
func TestItemUnchangedInventoryDoesNotRegrantArmor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, command string }{
		{"ender chest", "item replace entity @s enderchest.0 with minecraft:dirt"},
		{"cursor", "item replace entity @s player.cursor with minecraft:dirt"},
		{"crafting grid", "item replace entity @s player.crafting.0 with minecraft:dirt"},
		{"identical armor", "item replace entity @s armor.chest with minecraft:iron_chestplate"},
		{"unchanged modifier", `item modify entity @s armor.chest {type:"set_count",count:1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := joinOp(t, cfg, "Admin")
			servertest.HoldTicks(t, srv)
			c.ChatCommand("item replace entity @s armor.chest with minecraft:iron_chestplate")
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			c.ChatCommand("advancement revoke @s only minecraft:story/obtain_armor")
			c.Settle()
			c.ChatCommand(tc.command)
			feedback := "commands.item.entity.replace.success.single.known_item"
			if tc.name == "unchanged modifier" {
				feedback = "commands.item.entity.modify.success.single"
			}
			var chats []string
			var updates []sentUpdate
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlaySystemChat:
					tag, _, err := nbt.DecodeNetwork(data)
					if err != nil {
						t.Fatal(err)
					}
					message, _ := tag.(nbt.Compound)
					translation, _ := nbt.Get[nbt.String](message, "translate")
					chats = append(chats, string(translation))
				case protocol.IDPlayUpdateAdvancements:
					updates = append(updates, parseUpdate(t, data))
				}
				return false
			})
			if !slices.Equal(chats, []string{feedback}) {
				t.Errorf("unchanged inventory chat = %v, want only %s", chats, feedback)
			}
			if srv.AdvancementObtained("Admin", "minecraft:story/obtain_armor", "iron_chestplate") {
				t.Error("unchanged inventory regranted Suit Up")
			}
			for _, u := range updates {
				if got := u.progress["minecraft:story/obtain_armor"]["iron_chestplate"]; got != nil {
					t.Error("unchanged inventory sent an obtained Suit Up criterion")
				}
			}
		})
	}
}

// vanilla: AbstractContainerMenu.triggerSlotListeners, ItemStack.matches
func TestItemSlotChangesUpdateAdvancementSnapshot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, setup, command, advancement, criterion string }{
		{"count", "item replace entity @s hotbar.0 with minecraft:iron_ingot", "item replace entity @s hotbar.0 with minecraft:iron_ingot 2", "minecraft:story/smelt_iron", "iron"},
		{"component", "item replace entity @s armor.chest with minecraft:iron_chestplate", "item replace entity @s armor.chest with minecraft:iron_chestplate[damage=1]", "minecraft:story/obtain_armor", "iron_chestplate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := joinOp(t, cfg, "Admin")
			servertest.HoldTicks(t, srv)
			c.ChatCommand(tc.setup)
			c.Settle()
			c.ChatCommand("advancement revoke @s only " + tc.advancement)
			c.Settle()
			c.ChatCommand(tc.command)
			c.Settle()
			if !srv.AdvancementObtained("Admin", tc.advancement, tc.criterion) {
				t.Error("changed slot did not grant its advancement before the tick")
			}
			// No tick ran between the slot write and revoke: the command must
			// advance the shared snapshot itself to prevent a tick regrant.
			c.ChatCommand("advancement revoke @s only " + tc.advancement)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			if srv.AdvancementObtained("Admin", tc.advancement, tc.criterion) {
				t.Error("tick regranted an advancement after the command broadcast")
			}
		})
	}
}
