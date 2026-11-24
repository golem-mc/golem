package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The warden lifecycle and senses; vibration reception is in
// wardenvibration.go.
// vanilla: Warden, WardenAi

// TypeWarden is the warden.
const TypeWarden = registry.EntityTypeWarden

// vanilla: WardenAi, Warden.customServerAiStep
const (
	wardenEmergeTicks      = 134
	wardenDigTicks         = 100
	wardenDigCooldown      = 1200
	wardenDarknessInterval = 120
)

func init() {
	// vanilla: EntityTypes.WARDEN, Warden.createAttributes
	kinds[TypeWarden] = &kind{
		name: "Warden", width: float64(float32(0.9)), height: float64(float32(2.9)),
		maxHealth: 500, speed: float64(float32(0.3)), hostile: true,
		followRange: 24, attackDamage: 30, attackKnockback: 1.5,
		knockbackResist: 1, fireImmune: true, volume: 4,
		sounds: MobSounds{Ambient: registry.SoundEntityWardenAmbient, Hurt: registry.SoundEntityWardenHurt,
			Death: registry.SoundEntityWardenDeath, Step: registry.SoundEntityWardenStep, StepVolume: 10},
	}
	mobTracking[TypeWarden] = tracking{rangeChunks: 16, updateInterval: 3}
}

type wardenState struct {
	digCooldown, emerging               int64
	digging                             int
	pose                                int
	started                             bool
	anger                               []wardenSuspect
	hurt                                []wardenHurt
	clientAnger                         int
	conversionDelay                     int
	touchCooldown, sniffCooldown        int64
	sniffing                            bool
	sniffTicks, sensorDelay             int
	nearest                             int32
	disturbance                         Waypoint
	disturbanceTicks                    int
	roarTarget                          int32
	roarTicks, sonicTicks               int
	sonicCooldown                       int64
	roarSoundDelay, roarSoundCooldown   int64
	sonicSoundDelay, sonicSoundCooldown int64
	attackCooldown                      int
	roarPlayed, sonicFired              bool
	vibrationCooldown, recentProjectile int64
	vibration                           wardenVibration
	vibrationCandidate                  wardenVibration
	vibrateTicks                        int
	vibrating                           bool
	vibrationOfferedAge                 int64
	vibrationReload                     bool
	// Keep unmodelled memories so unloading does not destroy vanilla brain data.
	memories *memoryMap
}

// WardenPose is the warden's current animation pose.
func (m *Mob) WardenPose() int { return m.warden.pose }

// WardenEmerging prepares a triggered spawn before its pairing packets.
// vanilla: Warden.finalizeSpawn
func (m *Mob) WardenEmerging() {
	m.warden.emerging = wardenEmergeTicks
	m.SetMetadataWardenPose(protocol.PoseEmerging)
}

func (m *Mob) wardenUnderground() bool {
	return m.Type == TypeWarden && (m.warden.pose == protocol.PoseEmerging || m.warden.pose == protocol.PoseDigging)
}

// wardenStep runs the lifecycle before goals; true holds movement controls.
// vanilla: Warden.tick, WardenAi, Emerging, Digging
func (m *Mob) wardenStep(t *tickView) bool {
	s := &m.warden
	m.wardenVibrationTick(t)
	if (m.Persistence || m.requiresCustomPersistence()) && s.digCooldown > 0 {
		s.digCooldown = wardenDigCooldown
	}
	if m.NoAI {
		return m.wardenUnderground()
	}
	m.wardenCombatCooldowns()
	defer m.wardenAngerTick(t)
	m.wardenSensesTick(t)
	if (int64(m.Age)+int64(m.ID))%wardenDarknessInterval == 0 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobWardenDarkness, X: m.X, Y: m.Y, Z: m.Z, Radius: 20})
	}
	if s.digCooldown > 0 {
		s.digCooldown--
	}
	sound := func(sound registry.Sound) {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 5, Pitch: 1})
	}
	if s.pose == protocol.PoseEmerging {
		if !s.started {
			s.started = true
			sound(registry.SoundEntityWardenEmerge)
			return true
		}
		if s.emerging > 0 {
			s.emerging--
			return true
		}
		m.SetMetadataWardenPose(protocol.PoseStanding)
		s.started = false
	}
	if s.pose == protocol.PoseDigging {
		// Behavior.timedOut uses timestamp > endTimestamp.
		s.digging++
		if s.digging > wardenDigTicks {
			m.Discard()
		}
		return true
	}
	// DIG takes priority even while Digging is waiting for its start conditions.
	// vanilla: WardenAi.updateActivity, Digging.checkExtraStartConditions
	if s.digCooldown == 0 && s.roarTarget == 0 && m.TargetID == 0 {
		if !m.navDone() {
			return false
		}
		if m.OnGround {
			m.SetMetadataWardenPose(protocol.PoseDigging)
			s.digging = 0
			m.stopSeeking()
			sound(registry.SoundEntityWardenDig)
		} else if m.InWater() || m.InLava() {
			sound(registry.SoundEntityWardenAgitated)
			m.Discard()
		}
		return true
	}
	if held := m.wardenCombatStep(t); held || m.TargetID != 0 {
		return held
	}
	return m.wardenSniffStep(t)
}

// wardenFields reads and writes the lifecycle's expiring unit memories.
// vanilla: Brain.Packed, ExpirableValue.codec
func (m *Mob) wardenFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	s := &m.warden
	m.wardenAngerFields(out, load)
	keys := []struct {
		name  string
		ticks *int64
	}{
		{"minecraft:dig_cooldown", &s.digCooldown}, {"minecraft:is_emerging", &s.emerging},
		{"minecraft:sonic_boom_cooldown", &s.sonicCooldown},
		{"minecraft:sonic_boom_sound_delay", &s.sonicSoundDelay},
		{"minecraft:sonic_boom_sound_cooldown", &s.sonicSoundCooldown},
		{"minecraft:roar_sound_delay", &s.roarSoundDelay},
		{"minecraft:roar_sound_cooldown", &s.roarSoundCooldown},
		{"minecraft:touch_cooldown", &s.touchCooldown}, {"minecraft:sniff_cooldown", &s.sniffCooldown},
		{"minecraft:vibration_cooldown", &s.vibrationCooldown},
		{"minecraft:recent_projectile", &s.recentProjectile},
	}
	if load {
		brain, _ := nbt.Get[nbt.Compound](tag, "Brain")
		memories, _ := nbt.Get[nbt.Compound](brain, "memories")
		s.memories = m.loadMemoryMap(brain["memories"])
		m.SetMetadataWardenPose(protocol.PoseStanding)
		s.started, s.digging = false, 0
		for _, key := range keys {
			*key.ticks = 0
			memory, ok := nbt.Get[nbt.Compound](memories, key.name)
			if _, unit := nbt.CompoundView(memory["value"]); ok && unit {
				if ttl, ok := nbt.Get[nbt.Long](memory, "ttl"); ok && ttl > 0 {
					*key.ticks = int64(ttl)
				}
			}
			s.memories.Delete(key.name)
		}
		s.roarTarget, s.roarTicks, s.sonicTicks, s.attackCooldown = 0, 0, 0, 0
		s.roarPlayed, s.sonicFired = false, false
		s.sniffing = false
		if memory, ok := nbt.Get[nbt.Compound](memories, "minecraft:is_sniffing"); ok {
			_, s.sniffing = nbt.CompoundView(memory["value"])
		}
		s.memories.Delete("minecraft:is_sniffing")
		s.sniffTicks, s.sensorDelay, s.nearest, s.disturbanceTicks = 0, 0, 0, 0
		s.vibrating, s.vibrateTicks, s.vibrationReload = false, 0, false
		if listener, ok := nbt.Get[nbt.Compound](tag, "listener"); ok {
			candidate, current, travel := decodeWardenListener(listener)
			s.vibrationCandidate, s.vibration, s.vibrateTicks = candidate, current, travel
			s.vibrationOfferedAge, s.vibrating = candidate.tick, current.hasVibration
			s.vibrationReload = current.hasVibration
		}
		if s.emerging > 0 {
			m.SetMetadataWardenPose(protocol.PoseEmerging)
		}
		return
	}
	memories := m.cloneMemoryMap(s.memories)
	for _, key := range keys {
		if *key.ticks > 0 {
			memories.Set(key.name, memoryTag(nbt.NewOrderedCompound(0), *key.ticks))
		}
	}
	if s.sniffing {
		memories.Set("minecraft:is_sniffing", memoryTag(nbt.NewOrderedCompound(0)))
	}
	out.Set("Brain", brainTag(memories))
	current := s.vibration
	current.hasVibration = s.vibrating
	out.Set("listener", vibrationListenerTag(s.vibrationCandidate, current, s.vibrateTicks))
}

func wardenGoals(m *Mob) []Goal {
	return []Goal{newFloatGoal(m), &meleeAttackGoal{m: m, priority: 1, brain: true, followUnseen: true,
		pace: 1.2, interval: 18, swingStatus: 4, swingSound: registry.SoundEntityWardenAttackImpact,
		swingVolume: 10, swingPitch: m.VoicePitch,
		holdSwing: func() bool { return m.warden.attackCooldown > 0 },
		onSwing:   func() { m.warden.sonicCooldown = 40 },
	}, &wardenInvestigateGoal{m: m},
		// vanilla: WardenAi.initIdleActivity, the RunOne gated on IS_SNIFFING absent
		&gatedGoal{Goal: newBrainIdle(m, 10, 0.5, nil), enabled: func() bool { return !m.warden.sniffing }}}
}

func (m *Mob) wardenImmune(damageType registry.DamageType) bool {
	return m.wardenUnderground() && !rules.InvulnerabilityBypassed(damageType)
}

// WardenSuppressesShrieker reports whether the body intersects the shrieker's warning exclusion box.
// vanilla: WardenSpawnTracker.hasNearbyWarden
func (m *Mob) WardenSuppressesShrieker(x, y, z int32) bool {
	if m.Type != TypeWarden {
		return false
	}
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	x0, y0, z0, x1, y1, z1 := m.BlastBox()
	return x0 < cx+24 && x1 > cx-24 && y0 < cy+24 && y1 > cy-24 && z0 < cz+24 && z1 > cz-24
}
