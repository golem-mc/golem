package entity

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeTadpole is minecraft:tadpole.
const TypeTadpole = registry.EntityTypeTadpole

const tadpoleGrowTicks = 24000 // vanilla: Tadpole.ticksToBeFrog

func init() {
	// vanilla: EntityTypes.TADPOLE, Tadpole.createAttributes
	kinds[TypeTadpole] = &kind{
		name: "Tadpole", width: float64(float32(0.4)), height: float64(float32(0.3)), eyeHeight: float64(float32(0.19500001)),
		maxHealth: 6, speed: 1, water: true,
		temptTag: &registry.ItemTagIDFrogFood, temptPace: 1.25, panicPace: 2,
		sounds: MobSounds{Hurt: registry.SoundEntityTadpoleHurt, Death: registry.SoundEntityTadpoleDeath},
	}
	mobTracking[TypeTadpole] = tracking{rangeChunks: 10, updateInterval: 3}
}

type tadpoleState struct {
	age                         int
	up                          float64
	senseIn, temptationCooldown int
	tempting, lookID            int32
}

func (m *Mob) tadpoleFields(out nbt.CompoundTag, read bool) {
	tag := out.Entries()
	if read {
		age, _ := nbt.IntField(tag, "Age")
		m.tadpole.age = int(age)
		m.SetMetadataAgeLocked(nbt.BoolField(tag, "AgeLocked"))
	} else {
		out.Set("Age", nbt.Int(m.tadpole.age))
		out.Set("AgeLocked", boolTag(m.AgeLocked))
	}
}

// CanLockAge reports whether a golden dandelion can toggle the age lock: a
// live baby of an ageable kind outside #cannot_be_age_locked, off cooldown.
// vanilla: AgeableMob.canUseGoldenDandelion
func (m *Mob) CanLockAge() bool {
	if !m.Alive() || m.ageLockCooldown > 0 || (m.Type != TypeTadpole && (!Ageable(m.Type) || !m.IsBaby())) {
		return false
	}
	return !m.Type.InTag(registry.EntityTypeTagIDCannotBeAgeLocked)
}

// ToggleAgeLock flips the age lock, restarts babyhood and starts the
// cooldown; it reports whether the lock could be used.
// vanilla: AgeableMob.setAgeLockedData
func (m *Mob) ToggleAgeLock() bool {
	if !m.CanLockAge() {
		return false
	}
	m.SetMetadataAgeLocked(!m.AgeLocked)
	if m.Type == TypeTadpole {
		m.tadpole.age = 0
	} else {
		m.SetBaby(true)
	}
	m.ageLockCooldown = 40
	if m.AgeLocked {
		m.Persistence = true
	}
	return true
}

// vanilla: TadpoleAi.initCoreActivity, initIdleActivity
func tadpoleGoals(m *Mob) []Goal {
	return []Goal{
		&panicGoal{m: m, pace: 2, brain: true},
		&temptGoal{m: m, priority: 2, cooldownTicks: &m.tadpole.temptationCooldown, sensedPlayer: &m.tadpole.tempting},
		&swimmingIdleGoal{m: m, lookID: &m.tadpole.lookID},
		&timedPlayerLookGoal{m: m, priority: 7, reach: 6, lookID: &m.tadpole.lookID},
	}
}

func (m *Mob) tadpoleBrainStep(t *tickView) {
	a := &m.tadpole
	if a.temptationCooldown > 0 {
		a.temptationCooldown--
	}
	a.senseIn--
	if a.senseIn > 0 {
		return
	}
	a.senseIn, a.tempting = 20, 0 // vanilla: Sensor.SCAN_RATE
	if p := m.nearestTemptingPlayerWithin(t, m.temptRangeOr(temptRange)); p != nil {
		a.tempting = p.EntityID
	}
}

// vanilla: Brain.serialize, MemoryModuleType.TEMPTATION_COOLDOWN_TICKS
func (m *Mob) tadpoleBrainFields(out nbt.CompoundTag, read bool) {
	tag := out.Entries()
	if read {
		brain, _ := nbt.Get[nbt.Compound](tag, "Brain")
		memories, _ := nbt.Get[nbt.Compound](brain, "memories")
		cooldown, _ := nbt.Get[nbt.Compound](memories, "minecraft:temptation_cooldown_ticks")
		ticks, _ := nbt.IntValue(cooldown["value"])
		m.tadpole.temptationCooldown = int(ticks)
	} else {
		memories := m.newMemoryMap()
		if m.tadpole.temptationCooldown > 0 {
			memories.Set("minecraft:temptation_cooldown_ticks", memoryTag(nbt.Int(m.tadpole.temptationCooldown)))
		}
		out.Set("Brain", brainTag(memories))
	}
}

// vanilla: Tadpole.aiStep
func (m *Mob) tadpoleAgeStep() bool {
	if !m.Alive() {
		return false
	}
	if !m.AgeLocked {
		m.tadpole.age++
	}
	if m.tadpole.age >= tadpoleGrowTicks {
		m.gone = true
		return true
	}
	return false
}
