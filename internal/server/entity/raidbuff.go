package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// ApplyRaidBuffs is Raider.applyRaidBuffs for the pillager and the vindicator:
// on the raid's enchant odds the weapon gets the provider of the wave band
// (wave past the normal group count, past the easy one, or neither).
// vanilla: Pillager.applyRaidBuffs, Vindicator.applyRaidBuffs
func (m *Mob) ApplyRaidBuffs(wave, easyGroups, normalGroups int, enchantOdds float32) {
	enchanted := m.Rand.Float32() <= enchantOdds
	switch m.Type {
	case TypePillager:
		// Quick Charge I past the easy waves, II past the normal ones; the crossbow
		// is replaced only when one applies.
		provider := ""
		if wave > normalGroups {
			provider = "minecraft:raid/pillager_post_wave_5"
		} else if wave > easyGroups {
			provider = "minecraft:raid/pillager_post_wave_3"
		}
		if enchanted && provider != "" {
			m.SetEquipment(protocol.EquipMainHand, m.raidWeapon(registry.ItemCrossbow, provider))
		}
	case TypeVindicator:
		// A fresh axe replaces the one finalizeSpawn gave, enchanted or not.
		provider := "minecraft:raid/vindicator"
		if wave > normalGroups {
			provider = "minecraft:raid/vindicator_post_wave_5"
		}
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemIronAxe), Count: 1})
		if enchanted {
			m.SetEquipment(protocol.EquipMainHand, m.raidWeapon(registry.ItemIronAxe, provider))
		}
	}
}

// raidWeapon is a fresh item enchanted by the wave band's provider.
func (m *Mob) raidWeapon(item registry.Item, provider string) protocol.ItemStack {
	stack := protocol.ItemStack{ItemID: int32(item), Count: 1}
	enchantByKey(provider, diceEnchantRandom{GoDice{m.Rand}}, &stack, 0)
	return stack
}
