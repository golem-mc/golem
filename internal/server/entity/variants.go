package entity

import "github.com/golem-mc/golem/internal/server/rules"

// What the zombie, skeleton and spider variants add to their family's
// behaviour.

// IgnitesBlow reports whether the mob's blow can ignite its target: a
// burning zombie with an empty main hand. The caller holds the mob lock.
// vanilla: Zombie.doHurtTarget
func (m *Mob) IgnitesBlow() bool {
	return m.is(TypeZombie) && m.MainHand.Empty() && m.OnFire()
}

// BlowIgniteSeconds is the burn an igniting blow sets when roll (in [0,1))
// is under the effective difficulty; zero otherwise.
func BlowIgniteSeconds(effective, roll float32) int32 {
	if roll >= effective*0.3 {
		return 0
	}
	return 2 * int32(effective)
}

// BlowEffect is the status effect a monster's blow applies, by difficulty
// and effective difficulty; false when there is none.
func (m *Mob) BlowEffect(difficulty uint8, effective float32) (effect string, ticks int32, ok bool) {
	switch m.Type {
	case TypeHusk:
		// vanilla: Husk.doHurtTarget, with an empty main hand. The effective
		// difficulty is truncated; easy's 0.75 gives a zero-tick hunger, which
		// is left out.
		if !m.MainHand.Empty() {
			return "", 0, false
		}
		ticks = 140 * int32(effective)
		return "minecraft:hunger", ticks, ticks > 0
	case TypeBee:
		// vanilla: Bee.doHurtTarget, ten seconds on normal and eighteen on hard
		switch difficulty {
		case rules.DifficultyNormal:
			return "minecraft:poison", 10 * 20, true
		case rules.DifficultyHard:
			return "minecraft:poison", 18 * 20, true
		}
	case TypeWitherSkeleton:
		// vanilla: WitherSkeleton.doHurtTarget
		return "minecraft:wither", 200, true
	case TypeCaveSpider:
		// vanilla: CaveSpider.doHurtTarget, 7 seconds on normal, 15 on hard
		switch difficulty {
		case rules.DifficultyNormal:
			return "minecraft:poison", 7 * 20, true
		case rules.DifficultyHard:
			return "minecraft:poison", 15 * 20, true
		}
	}
	return "", 0, false
}
