package commands

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LivingEntity.onEquipItem, Player.doesEmitEquipEvent
func TestItemReplaceHeadMismatchedEquippableEvent(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	var mu sync.Mutex
	var events []string
	srv.ObserveGameEvents(func(event string, _, _, _ float64) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	c.ChatCommand("item replace entity @s armor.head with minecraft:elytra")
	if heard, _, _ := servertest.CollectSoundsSettled(c); len(heard) != 0 {
		t.Errorf("mismatched slot sounds = %+v, want silence", heard)
	}
	if got := srv.InventorySlot("Testificate", player.ArmorSlot); registry.Item(got.ItemID) != registry.ItemElytra || got.Count != 1 {
		t.Errorf("head = %+v, want one elytra", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(events, []string{"minecraft:equip"}) {
		t.Errorf("events = %v, want [minecraft:equip]", events)
	}
}

// vanilla: LivingEntity.setItemSlot, LivingEntity.onEquipItem
func TestItemReplaceArmorEquipSound(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	for _, tc := range []struct {
		name, command, sound string
		slot                 int
		item                 registry.Item
	}{
		{"elytra", `item replace entity @s armor.chest with minecraft:elytra[enchantments={"minecraft:protection":1}]`, "minecraft:item.armor.equip_elytra", player.ArmorSlot + 1, registry.ItemElytra},
		{"identical elytra", `item replace entity @s armor.chest with minecraft:elytra[enchantments={"minecraft:protection":1}]`, "", player.ArmorSlot + 1, registry.ItemElytra},
		{"changed components", `item replace entity @s armor.chest with minecraft:elytra[enchantments={"minecraft:protection":2}]`, "minecraft:item.armor.equip_elytra", player.ArmorSlot + 1, registry.ItemElytra},
		{"chestplate", "item replace entity @s armor.chest with minecraft:iron_chestplate", "minecraft:item.armor.equip_iron", player.ArmorSlot + 1, registry.ItemIronChestplate},
		{"helmet", "item replace entity @s armor.head with minecraft:diamond_helmet", "minecraft:item.armor.equip_diamond", player.ArmorSlot, registry.ItemDiamondHelmet},
		{"leggings", "item replace entity @s armor.legs with minecraft:leather_leggings", "minecraft:item.armor.equip_leather", player.ArmorSlot + 2, registry.ItemLeatherLeggings},
		{"boots", "item replace entity @s armor.feet with minecraft:golden_boots", "minecraft:item.armor.equip_gold", player.ArmorSlot + 3, registry.ItemGoldenBoots},
		{"held armor", "item replace entity @s weapon.mainhand with minecraft:elytra", "", player.HotbarSlot, registry.ItemElytra},
		{"copy into armor", "item replace entity @s armor.chest from entity @s weapon.mainhand", "minecraft:item.armor.equip_elytra", player.ArmorSlot + 1, registry.ItemElytra},
		{"read armor source", "item replace entity @s hotbar.1 from entity @s armor.chest", "", player.HotbarSlot + 1, registry.ItemElytra},
		{"custom sound", `item replace entity @s armor.head with minecraft:stick[equippable={slot:"head",equip_sound:"minecraft:block.amethyst_block.chime"}]`, "minecraft:block.amethyst_block.chime", player.ArmorSlot, registry.ItemStick},
		{"unequip", "item replace entity @s armor.chest with minecraft:air", "", player.ArmorSlot + 1, registry.ItemAir},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			heard, _, _ := servertest.CollectSoundsSettled(c)
			if tc.sound == "" {
				if len(heard) != 0 {
					t.Errorf("sounds = %+v, want silence", heard)
				}
			} else if len(heard) != 1 || heard[0].Name != tc.sound || heard[0].Category != protocol.SoundCategoryPlayer || heard[0].Volume != 1 || heard[0].Pitch != 1 {
				t.Errorf("sounds = %+v, want %s once in players at volume/pitch 1", heard, tc.sound)
			}
			got := srv.InventorySlot("Testificate", tc.slot)
			if tc.item == registry.ItemAir {
				if !got.Empty() {
					t.Errorf("slot %d = %+v, want empty", tc.slot, got)
				}
			} else if registry.Item(got.ItemID) != tc.item || got.Count != 1 {
				t.Errorf("slot %d = %+v, want one item %d", tc.slot, got, tc.item)
			}
		})
	}

	c.ChatCommand("gamemode spectator")
	c.Settle()
	c.ChatCommand("item replace entity @s armor.chest with minecraft:elytra")
	if heard, _, _ := servertest.CollectSoundsSettled(c); len(heard) != 0 {
		t.Errorf("spectator sounds = %+v, want silence", heard)
	}
}
