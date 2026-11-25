package entity

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// zombieCure holds ZombieVillager's conversion countdown and its player credit.
type zombieCure struct {
	active bool
	left   int32
	player uuid.UUID
}

const (
	cureWeakness = registry.MobEffectWeakness
	cureStrength = registry.MobEffectStrength
)

// cureSpeedUp is the blocks near a curing zombie villager that hurry it.
var cureSpeedUp = registry.BlockTagOf("minecraft:speeds_up_zombie_villager_curing")

// Curing reports whether a zombie villager is becoming a villager.
func (m *Mob) Curing() bool { return m.Type == TypeZombieVillager && m.cure.active }

// CurePlayer is the player who started the conversion, or the zero UUID.
func (m *Mob) CurePlayer() uuid.UUID { return m.cure.player }

// CureZombie starts a weak zombie villager's cure with a golden apple.
// vanilla: ZombieVillager.mobInteract
func (m *Mob) CureZombie(player uuid.UUID, difficulty uint8) ([]effect.Change, bool) {
	if m.Type != TypeZombieVillager || !m.Alive() {
		return nil, false
	}
	if _, weak := m.effects.Get(cureWeakness); !weak {
		return nil, false
	}
	return m.startCuring(player, int32(m.Rand.Intn(2401)+3600), difficulty), true
}

// resetCure clears the cure state and the synched curing flag.
func (m *Mob) resetCure() {
	m.cure = zombieCure{}
	m.InvalidateMetadataCure()
}

// vanilla: ZombieVillager.startConverting
func (m *Mob) startCuring(player uuid.UUID, ticks int32, difficulty uint8) []effect.Change {
	m.cure = zombieCure{active: true, left: ticks, player: player}
	m.InvalidateMetadataCure()
	var changes []effect.Change
	if ch, ok := m.RemoveEffect(cureWeakness); ok {
		changes = append(changes, ch)
	}
	if ch, ok := m.AddEffect(effect.New(cureStrength, ticks, int32(min(int(difficulty)-1, 0)), false, true, true)); ok {
		changes = append(changes, ch)
	}
	return changes
}

// cureStep runs even with NoAI. Coordinates deliberately truncate toward zero.
// vanilla: ZombieVillager.tick, getConversionProgress
func (m *Mob) cureStep(w World) bool {
	if !m.Curing() {
		return false
	}
	amount := int32(1)
	if m.Rand.Float32() < 0.01 {
		special := 0
		x, y, z := int32(m.X), int32(m.Y), int32(m.Z)
		for xx := x - 4; xx < x+4 && special < 14; xx++ {
			for yy := y - 4; yy < y+4 && special < 14; yy++ {
				for zz := z - 4; zz < z+4 && special < 14; zz++ {
					b := registry.BlockOf(w.BlockAt(xx, yy, zz))
					if !b.InTag(cureSpeedUp) {
						continue
					}
					if m.Rand.Float32() < 0.3 {
						amount++
					}
					special++
				}
			}
		}
	}
	m.cure.left -= amount
	return m.cure.left <= 0
}

// CuredFrom transfers a zombie villager's trades and opinions, returning the
// preserved equipment to drop; binding equipment goes in inventory slots.
// vanilla: ZombieVillager.finishConversion
func (m *Mob) CuredFrom(old *Mob) []protocol.ItemStack {
	m.transferVillagerData(old)
	drops := old.takePreservedEquipment(func(slot int, stack protocol.ItemStack) bool {
		if !player.HasBindingCurse(stack) {
			return true
		}
		// EquipmentSlot.getIndex is local to the slot type, not its ordinal:
		// feet and mainhand both address inventory slot zero.
		index := [...]int{0, 1, 0, 1, 2, 3, 0, 0}[slot]
		m.barter.Inventory[index] = stack
		return false
	})
	m.AddEffect(effect.New(registry.MobEffectNausea, 200, 0, false, true, true))
	return drops
}

// vanilla: ZombieVillager.addAdditionalSaveData
func (m *Mob) saveCure(out nbt.CompoundTag) {
	ticks := int32(-1)
	if m.cure.active {
		ticks = m.cure.left
	}
	m.saveGossip(out)
	out.Set("ConversionTime", nbt.Int(ticks))
	out.Delete("ConversionPlayer")
	if m.cure.player != uuid.Nil {
		out.Set("ConversionPlayer", nbt.UUIDToIntArray(m.cure.player))
	}
	out.Set("Xp", nbt.Int(m.VillagerXp))
}

// vanilla: ZombieVillager.readAdditionalSaveData
func (m *Mob) loadCure(tag nbt.Compound, difficulty uint8) {
	m.loadGossip(tag)
	ticks := int32(-1)
	if v, ok := nbt.IntField(tag, "ConversionTime"); ok {
		ticks = v
	}
	m.resetCure()
	if ticks != -1 {
		player, _ := nbt.UUIDField(tag, "ConversionPlayer")
		m.startCuring(player, ticks, difficulty)
	}
	m.VillagerXp, _ = nbt.IntField(tag, "Xp")
}

// InfectedFrom initializes an infected zombie and restores its villager data.
// vanilla: Zombie.convertVillagerToZombieVillager
func (m *Mob) InfectedFrom(old *Mob, special float32) {
	// CONVERSION retains loot pickup, age and equipment.
	dice := m.SpawnDice()
	m.finalizeMob(dice)
	m.rollBreakDoors(dice, special)
	m.RollConversionAttributes(special)
	m.SetMetadataHealth(m.MaxHealth)
	m.transferVillagerData(old)
	m.SetMetadataVillagerFinalized(true)
}

func (m *Mob) transferVillagerData(old *Mob) {
	m.Villager, m.VillagerXp = old.Villager, old.VillagerXp
	m.InvalidateMetadataVillager()
	if old.Offers != nil {
		m.Offers = nbt.Copy(old.Offers).(nbt.Compound)
	}
	m.vg.gossips = maps.Clone(old.vg.gossips)
}

// Infects reports whether m's kill of a villager converts it: always on hard,
// by a coin flip on normal, never otherwise.
// vanilla: Zombie.killedEntity
func (m *Mob) Infects(victim *Mob, difficulty uint8) bool {
	if m.family() != TypeZombie || victim.Type != TypeVillager {
		return false
	}
	switch difficulty {
	case rules.DifficultyHard:
		return true
	case rules.DifficultyNormal:
		return m.Rand.Intn(2) != 0 // nextBoolean true converts
	}
	return false
}
