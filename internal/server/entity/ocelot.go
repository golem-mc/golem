package entity

import (
	"github.com/golem-mc/golem/registry"
)

// ocelotKind is the ocelot's table entry: the cat's box, health, pace and
// baby, with eyes at the default 0.85 of the height and the ocelot's own
// sounds.
// vanilla: EntityTypes.OCELOT's builder, Ocelot.createAttributes
var ocelotKind = &kind{
	name:         "Ocelot",
	fireAverse:   true,
	width:        0.6,
	height:       0.7,
	maxHealth:    10,
	speed:        float64(float32(0.3)),
	attackDamage: 3,
	babyWidth:    0.3,
	babyHeight:   0.35,
	babyEye:      0.34375,
	temptTag:     &registry.ItemTagIDOcelotFood,
	sounds: MobSounds{
		Ambient: registry.SoundEntityOcelotAmbient,
		Hurt:    registry.SoundEntityOcelotHurt,
		Death:   registry.SoundEntityOcelotDeath,
	},

	temptPace:       0.6, // Ocelot.CROUCH_SPEED_MOD
	strollPace:      0.8, // Ocelot.WALK_SPEED_MOD
	ambientInterval: 900, // Ocelot.getAmbientSoundInterval
}

// The ocelot's goal priorities.
// vanilla: Ocelot.registerGoals
const (
	ocelotPriorityTempt  = 3
	ocelotPriorityAvoid  = 4
	ocelotPriorityLeap   = 7
	ocelotPriorityAttack = 8
	ocelotPriorityHunt   = 1
	ocelotPriorityBreed  = 9
	ocelotPriorityStroll = 10
	ocelotPriorityLookAt = 11
)

// ocelotGoals wires float, the fish tempt, fleeing players until trusting,
// the leap and bite, breeding, stroll and glance, and the hunts of chickens
// and baby turtles ashore.
// vanilla: Ocelot.registerGoals
func ocelotGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&mobTargetGoal{m: m, priority: ocelotPriorityHunt, victim: TypeChicken, unsighted: true},
		&mobTargetGoal{m: m, priority: ocelotPriorityHunt, unsighted: true, matches: isBabyTurtleAshore},
		&temptGoal{m: m, priority: ocelotPriorityTempt, canScare: true, scareOff: func() bool { return m.Trusting }},
		newFelineAvoidGoal(m, ocelotPriorityAvoid, func() bool { return !m.Trusting }),
		&leapAtTargetGoal{m: m, priority: ocelotPriorityLeap, yd: catLeapUp},
		&felineAttackGoal{m: m, priority: ocelotPriorityAttack},
		&breedGoal{m: m, priority: ocelotPriorityBreed},
		&randomStroll{m: m, priority: ocelotPriorityStroll},
		&lookAtPlayer{m: m, priority: ocelotPriorityLookAt, reach: 10}, // vanilla: Ocelot.registerGoals
	}
}

// TryToTrust is the fish's roll: one time in three the ocelot trusts players
// from then on. It reports whether it did.
// vanilla: Ocelot.mobInteract
func (m *Mob) TryToTrust() bool {
	if m.Rand.Intn(tameOdds) != 0 {
		return false
	}
	m.SetMetadataTrusting(true)
	return true
}

// TemptRunning reports whether a lure is leading the mob, the condition an
// ocelot needs to take a fish.
// vanilla: Ocelot.mobInteract's temptGoal.isRunning
func (m *Mob) TemptRunning() bool {
	for _, g := range m.ai.running {
		if _, ok := g.(*temptGoal); ok {
			return true
		}
	}
	return false
}

// ocelotSpawnOk is the ocelot's spawn rule: two in three, at or above sea
// level, over grass or leaves.
// vanilla: Ocelot.checkOcelotSpawnRules, Ocelot.checkSpawnObstruction
func ocelotSpawnOk(w SpawnWorld, rng Dice, x, y, z int32) bool {
	return rng.NextIntN(3) != 0 && ocelotSpawnGroundOk(w, x, y, z)
}

// ocelotSpawnGroundOk checks the height and ground shared by natural and spawner spawns.
// vanilla: Ocelot.checkSpawnObstruction
func ocelotSpawnGroundOk(w SpawnWorld, x, y, z int32) bool {
	if y < w.SeaLevel() {
		return false
	}
	below := registry.BlockOf(w.BlockAt(x, y-1, z))
	return below == registry.BlockGrassBlock || below.InTag(leavesBlocks)
}
