package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TypeCamelHusk is minecraft:camel_husk.
const TypeCamelHusk = registry.EntityTypeCamelHusk

func init() {
	// vanilla: EntityTypes.CAMEL_HUSK, a MONSTER Camel without a baby or a mate
	kinds[TypeCamelHusk] = &kind{
		fireAverse: true,
		name:       "Camel Husk", width: float64(float32(1.7)), height: 2.375, eyeHeight: float64(float32(2.275)),
		hostile: true, livesInPeaceful: true, maxHealth: 32, speed: float64(float32(0.09)), stepHeight: 1.5,
		temptTag: &registry.ItemTagIDCamelHuskFood, temptPace: 2.5, followPace: 2.5, panicPace: 4, strollPace: 2,
		volume: 0.8, ambientInterval: 400,
		sounds: MobSounds{Ambient: registry.SoundEntityCamelHuskAmbient, Hurt: registry.SoundEntityCamelHuskHurt, Death: registry.SoundEntityCamelHuskDeath, Eat: registry.SoundEntityCamelHuskEat, Step: registry.SoundEntityCamelHuskStep},
	}
	mobTracking[TypeCamelHusk] = mobTracking[TypeCamel]
}

// IsCamel reports the camel and the camel husk.
func IsCamel(t MobType) bool { return t == TypeCamel || t == TypeCamelHusk }

// MobControlled reports a camel husk whose first passenger is a mob: a player
// rider sits ahead of the mobs in the seating order.
// vanilla: CamelHusk.isMobControlled
func (m *Mob) MobControlled() bool {
	return m.Type == TypeCamelHusk && m.Horse.Rider == uuid.Nil && len(m.Passengers) != 0
}

// CamelVoice identifies a camel action's sound.
type CamelVoice uint8

const (
	CamelDash CamelVoice = iota
	CamelDashReady
	CamelSaddle
	CamelSit
	CamelStand
)

var camelVoices = [2][5]registry.Sound{
	{registry.SoundEntityCamelDash, registry.SoundEntityCamelDashReady, registry.SoundEntityCamelSaddle, registry.SoundEntityCamelSit, registry.SoundEntityCamelStand},
	{registry.SoundEntityCamelHuskDash, registry.SoundEntityCamelHuskDashReady, registry.SoundEntityCamelHuskSaddle, registry.SoundEntityCamelHuskSit, registry.SoundEntityCamelHuskStand},
}

// CamelSound selects the camel's or camel husk's action sound.
func (m *Mob) CamelSound(event CamelVoice) registry.Sound {
	kind := 0
	if m.Type == TypeCamelHusk {
		kind = 1
	}
	return camelVoices[kind][event]
}

// camelHuskJockeyOdds is the chance a natural husk rides a camel husk.
// vanilla: Husk.finalizeSpawn
const camelHuskJockeyOdds = 0.1

// finalizeHusk is Husk.finalizeSpawn after Zombie's, on the same dice: a
// second loot-pickup roll, then a natural husk's roll for an iron spear and a
// camel husk with a parched behind it. A spot where the camel husk's box is
// blocked (CamelHuskFits false) draws nothing. Every natural husk rolls, not
// only a pack's first: vanilla wraps the group in a fresh HuskGroupData each
// finalize, so triedToSpawnCamelHusk never carries to the next member.
// vanilla: Husk.finalizeSpawn
func (m *Mob) finalizeHusk(dice Dice, group *SpawnGroup, local rules.LocalDifficulty) {
	m.CanPickUpLoot = dice.NextFloat() < lootPickupOdds*local.Special
	if group == nil || !group.natural || !m.CamelHuskFits {
		return
	}
	if dice.NextFloat() < camelHuskJockeyOdds {
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemIronSpear), Count: 1})
		m.Jockey = JockeyCamelHusk
	}
}

// CamelHuskSpawnBox is EntityType.getSpawnAABB for a camel husk (1.7 x 2.375)
// at the husk's block, which finalizeSpawn tests for a free spot.
func CamelHuskSpawnBox(x, y, z int32) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	const halfWidth, height = 1.7 / 2, 2.375
	cx, cz := float64(x)+0.5, float64(z)+0.5
	return cx - halfWidth, float64(y), cz - halfWidth, cx + halfWidth, float64(y) + height, cz + halfWidth
}
