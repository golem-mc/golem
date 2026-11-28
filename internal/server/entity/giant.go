package entity

import "github.com/golem-mc/golem/registry"

// TypeGiant is minecraft:giant.
const TypeGiant = registry.EntityTypeGiant

func init() {
	// vanilla: EntityTypes.GIANT's builder and Giant.createAttributes. Giant
	// registers no goals, so it stands still.
	kinds[TypeGiant] = &kind{
		name: "Giant", width: float64(float32(3.6)), height: 12, eyeHeight: float64(float32(10.44)),
		hostile: true, maxHealth: 100, speed: 0.5, followRange: 16, attackDamage: 50, cameraDistance: 16,
		// vanilla: Monster.getHurtSound, getDeathSound
		sounds: MobSounds{
			Hurt:  registry.SoundEntityHostileHurt,
			Death: registry.SoundEntityHostileDeath,
		},
	}
	mobTracking[TypeGiant] = tracking{rangeChunks: 10, updateInterval: 3}
}
