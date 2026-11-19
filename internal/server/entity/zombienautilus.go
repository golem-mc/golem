package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TypeZombieNautilus is minecraft:zombie_nautilus.
const TypeZombieNautilus = registry.EntityTypeZombieNautilus

func init() {
	// vanilla: EntityTypes.ZOMBIE_NAUTILUS, ZombieNautilus.createAttributes
	kinds[TypeZombieNautilus] = &kind{
		name: "Zombie Nautilus", width: 0.875, height: float64(float32(0.95)), eyeHeight: float64(float32(0.2751)),
		hostile: true, livesInPeaceful: true, burnsInDaylight: true, maxHealth: 15, speed: float64(float32(1.1)), attackDamage: 3, knockbackResist: float64(float32(0.3)),
		temptTag: &registry.ItemTagIDNautilusFood, temptPace: 0.9, strollPace: 1, ambientInterval: 120,
		sounds: MobSounds{Ambient: registry.SoundEntityZombieNautilusAmbient}, // see TypeNautilus
	}
	mobTracking[TypeZombieNautilus] = tracking{rangeChunks: 10, updateInterval: 3}
}

// IsNautilus reports the kinds inheriting AbstractNautilus's controls.
func IsNautilus(t MobType) bool { return t == TypeNautilus || t == TypeZombieNautilus }

// finalizeDrowned is Drowned.finalizeSpawn after Zombie's: the always-dropping
// off-hand shell, then the nautilus jockey.
// vanilla: Drowned.finalizeSpawn
func (m *Mob) finalizeDrowned(dice Dice, biome registry.Biome, group *SpawnGroup, structure bool) {
	if m.OffHand.Empty() && dice.NextFloat() < drownedShellOdds {
		m.SetEquipment(protocol.EquipOffHand, protocol.ItemStack{ItemID: int32(registry.ItemNautilusShell), Count: 1})
		m.DropChances[1] = guaranteedDropChance
	}
	m.rollNautilusJockey(dice, biome, group, structure)
}

// vanilla: Drowned.finalizeSpawn
func (m *Mob) rollNautilusJockey(dice Dice, biome registry.Biome, group *SpawnGroup, structure bool) {
	if (!structure && (group == nil || !group.natural)) || registry.Item(m.MainHand.ItemID) != registry.ItemTrident || dice.NextFloat() >= 0.5 || m.IsBaby() || biome.InTag(registry.BiomeTagIDMoreFrequentDrownedSpawns) {
		return
	}
	m.Jockey = JockeyZombieNautilus
	if structure {
		m.Jockey = JockeyStructureNautilus
	}
}
