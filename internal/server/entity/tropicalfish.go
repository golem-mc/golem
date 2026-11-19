package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EntityTypes.TROPICAL_FISH, AbstractFish.createAttributes
func init() {
	kinds[TypeTropicalFish] = &kind{
		name: "Tropical Fish", width: 0.5, height: 0.4, eyeHeight: 0.26,
		maxHealth: 3, speed: 0.7, water: true,
		sounds: MobSounds{
			Ambient: registry.SoundEntityTropicalFishAmbient,
			Hurt:    registry.SoundEntityTropicalFishHurt,
			Death:   registry.SoundEntityTropicalFishDeath,
		},
		ambientInterval: 120,
	}
	mobTracking[TypeTropicalFish] = tracking{rangeChunks: 4, updateInterval: 3}
}

// vanilla: TropicalFish.COMMON_VARIANTS, in selection order; low 16 bits are
// the pattern, followed by the base and pattern DyeColor ids.
var tropicalCommonVariants = [...]int32{
	0x07010101, 0x07070001, 0x0b070001, 0x07000501,
	0x070b0100, 0x00010000, 0x03060500, 0x040a0301,
	0x0e000501, 0x04000500, 0x07000201, 0x01000501,
	0x06090300, 0x03050400, 0x000e0401, 0x0e070200,
	0x000e0301, 0x04000001, 0x000e0000, 0x00070100,
	0x04090300, 0x04040001,
}

// NormalizeTropicalVariant applies TropicalFish.Variant's codec fallbacks.
func NormalizeTropicalVariant(v int32) int32 {
	pattern := v & 0xffff
	if pattern&0xff > 1 || pattern>>8 > 5 {
		pattern = 0
	}
	base, color := v>>16&0xff, v>>24&0xff
	if base > 15 {
		base = 0
	}
	if color > 15 {
		color = 0
	}
	return pattern | base<<16 | color<<24
}

// vanilla: TropicalFish.finalizeSpawn
func (m *Mob) finalizeTropicalFish(group *SpawnGroup) {
	if group != nil && group.tropicalSet {
		m.SetMetadataTropicalVariant(group.tropicalVariant)
		return
	}
	if float64(m.Rand.Float32()) < 0.9 {
		m.SetMetadataTropicalVariant(tropicalCommonVariants[m.Rand.Intn(len(tropicalCommonVariants))])
		if group != nil {
			group.tropicalVariant, group.tropicalSet = m.TropicalVariant, true
		}
		return
	}
	// Pattern.values(): six small patterns, then six large patterns.
	p := m.Rand.Intn(12)
	base, color := m.Rand.Intn(16), m.Rand.Intn(16)
	m.SetMetadataTropicalVariant(int32(p/6 | (p%6)<<8 | base<<16 | color<<24))
	if group != nil {
		group.tropicalSolitary = true
	}
}

// vanilla: TropicalFish.checkTropicalFishSpawnRules, SpawnPlacementTypes.IN_WATER
func tropicalFishSpawnPositionOK(w SpawnWorld, x, y, z int32) bool {
	if !rules.IsWater(w.BlockAt(x, y, z)) || !rules.IsWater(w.BlockAt(x, y-1, z)) ||
		!rules.IsWaterBlock(w.BlockAt(x, y+1, z)) {
		return false
	}
	return w.Biome(x, y, z).InTag(registry.BiomeTagIDAllowsTropicalFishSpawnsAtAnyHeight) ||
		(y >= w.SeaLevel()-13 && y <= w.SeaLevel())
}
