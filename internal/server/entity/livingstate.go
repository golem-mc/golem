package entity

import "github.com/golem-mc/golem/nbt"

// LivingState holds the saved timers, absorption and flight flag shared by living entities.
type LivingState struct {
	HurtTime, DeathTime          int
	FallFlying                   bool
	AbsorptionAmount             float32
	ImpulseContextResetGraceTime int
}

// SaveTo writes the shared LivingEntity fields; a body's brain writer may replace Brain.
func (s *LivingState) SaveTo(tag nbt.CompoundTag) {
	s.SaveWithAttributes(tag, nil)
}

// SaveWithAttributes inserts the body's attributes, effects and impact before flight and brain.
func (s *LivingState) SaveWithAttributes(tag nbt.CompoundTag, writeAttributes func()) {
	s.SaveWithSleepingPos(tag, writeAttributes, nil)
}

// SaveWithSleepingPos writes an optional bed position before the brain.
func (s *LivingState) SaveWithSleepingPos(tag nbt.CompoundTag, writeAttributes func(), sleepingPos nbt.IntArray) {
	s.fields(mobCodec{tag: tag.Entries(), out: tag}, writeAttributes)
	if sleepingPos != nil {
		tag.Set("sleeping_pos", sleepingPos)
	}
	tag.Set("Brain", brainTag(nil))
}

// LoadFrom reads the shared LivingEntity fields, defaulting absent timers to zero.
func (s *LivingState) LoadFrom(tag nbt.Compound) {
	s.fields(mobCodec{tag: tag, load: true}, nil)
}

// vanilla: LivingEntity.addAdditionalSaveData, readAdditionalSaveData
func (s *LivingState) fields(c mobCodec, writeAttributes func()) {
	c.short("HurtTime", &s.HurtTime)
	c.short("DeathTime", &s.DeathTime)
	if c.load {
		s.AbsorptionAmount, _ = nbt.FloatField(c.tag, "AbsorptionAmount")
	} else {
		c.set("AbsorptionAmount", nbt.Float(s.AbsorptionAmount))
	}
	mobInteger(&c, "current_impulse_context_reset_grace_time", &s.ImpulseContextResetGraceTime, mobDefaultZero)
	if writeAttributes != nil {
		writeAttributes()
	}
	c.flag("FallFlying", &s.FallFlying, 0)
}

func (c *mobCodec) short(key string, v *int) {
	if c.load {
		n, _ := nbt.ShortField(c.tag, key)
		*v = int(n)
	} else {
		c.set(key, nbt.Short(*v))
	}
}

// TickTimers advances the living countdowns even for a body without AI or a dying body.
// vanilla: LivingEntity.baseTick, tick
func (s *LivingState) TickTimers() {
	if s.HurtTime > 0 {
		s.HurtTime--
	}
	if s.ImpulseContextResetGraceTime > 0 {
		s.ImpulseContextResetGraceTime--
	}
}
