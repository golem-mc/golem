package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TypeZombifiedPiglin is the zombified piglin, a Zombie subclass and a
// NeutralMob.
const TypeZombifiedPiglin = registry.EntityTypeZombifiedPiglin

// piglinState is the zombified piglin's own state.
type piglinState struct {
	firstAngerSoundIn int
	// alertIn counts the ticks until the next alert of the other piglins.
	alertIn int
	// hadTarget is whether the last tick ended with a target, to see a new
	// one arrive.
	hadTarget bool
}

// The alert's interval, TimeUtil.rangeOfSeconds(4, 6), in ticks.
// vanilla: ZombifiedPiglin.ALERT_INTERVAL
const (
	piglinAlertMin = 4 * 20
	piglinAlertMax = 6 * 20
)

// piglinGoals is ZombifiedPiglin.registerGoals with Zombie's looks: the
// grudge alerts its kind, and the nearest scan targets only players it is
// angry at.
func piglinGoals(m *Mob) []Goal {
	return []Goal{
		newZombieBreakDoorGoal(m),
		&hurtByGoal{m: m, priority: priorityHurtBy, alertOthers: true},
		&nearestTargetGoal{m: m, priority: priorityNearest, accept: func(t *tickView, p *Player) bool {
			return m.angryAt(t.w, p)
		}},
		&meleeAttackGoal{m: m, priority: priorityNearest},
		newZombieAttackTurtleEggGoal(m),
		&resetAngerGoal{m: m, alertSame: true},
		&randomStroll{m: m, priority: hostileStroll},
		&lookAtPlayer{m: m, priority: hostileLookAt},
		&randomLookAround{m: m, priority: hostileLookAround},
	}
}

// piglinAlertStep is ZombifiedPiglin.maybeAlertOthers: with a visible
// target, every four to six seconds it sends targetless piglins in follow
// range after it.
// vanilla: ZombifiedPiglin.maybeAlertOthers, alertOthers
func (m *Mob) piglinAlertStep(t *tickView) {
	has := m.TargetID != 0
	if has && !m.piglin.hadTarget {
		// vanilla: ZombifiedPiglin.setTarget, FIRST_ANGER_SOUND_DELAY
		m.piglin.firstAngerSoundIn = m.Rand.Intn(21)
		m.piglin.alertIn = m.piglinAlertInterval()
	}
	m.piglin.hadTarget = has
	// A zero sample stays silent; losing the target does not cancel the
	// countdown while persistent anger lasts.
	// vanilla: ZombifiedPiglin.maybePlayFirstAngerSound, playAngerSound
	if m.Angry() && m.piglin.firstAngerSoundIn > 0 {
		m.piglin.firstAngerSoundIn--
		if m.piglin.firstAngerSoundIn == 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded,
				SoundID: registry.SoundEntityZombifiedPiglinAngry, Volume: m.SoundVolume() * 2, Pitch: m.VoicePitch() * 1.8})
		}
	}
	if !has {
		return
	}
	if m.piglin.alertIn > 0 {
		m.piglin.alertIn--
		return
	}
	if p := m.targetIn(t); p != nil && m.seesTarget(t.w, p) {
		for _, o := range t.others {
			if o == m || o.Type != TypeZombifiedPiglin || o.TargetID != 0 || !o.Alive() || !alertReaches(m, o) {
				continue
			}
			o.SetMetadataTargetID(m.TargetID)
			o.alerted = m.TargetID
		}
	}
	m.piglin.alertIn = m.piglinAlertInterval()
}

func (m *Mob) piglinAlertInterval() int {
	return piglinAlertMin + m.Rand.Intn(piglinAlertMax-piglinAlertMin+1)
}

// rollPiglinHand is ZombifiedPiglin.populateDefaultEquipmentSlots: a golden
// sword in the main hand, a golden spear one time in twenty, and no armour.
func (m *Mob) rollPiglinHand(dice Dice) {
	tool := int32(registry.ItemGoldenSword)
	if dice.NextIntN(20) == 0 {
		tool = int32(registry.ItemGoldenSpear)
	}
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: tool, Count: 1})
}

// EquipPiglinHand arms a body converted into a zombified piglin with the
// default hand and nothing worn.
// vanilla: Pig.thunderHit, ZombifiedPiglin.populateDefaultEquipmentSlots
func (m *Mob) EquipPiglinHand() {
	m.rollPiglinHand(m.equipmentDice())
}
