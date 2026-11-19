package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// WaterSensitiveStep bites a water-sensitive body that stands in water or
// rain, whether or not its AI runs.
// vanilla: LivingEntity.aiStep
func (m *Mob) WaterSensitiveStep(w World, events []MobEvent) []MobEvent {
	if m.Dead || !m.kind().waterSensitive {
		return events
	}
	if m.InWater() || rules.RainsOnBody(w, m.X, m.Y, m.Z, m.Y+m.height()) {
		events = m.bite(rules.DamageTypeDrown, events)
	}
	return events
}

// HurtByWaterSplash is a water bottle's shatter reaching a water-sensitive
// body: one point of indirect magic, then an enderman's 64 teleport tries.
// The caller has checked the 4-block reach. Landed means the damage got through
// the hurt window, not that the enderman teleported; softened is the window's.
// vanilla: AbstractThrownPotion.onHitAsWater, EnderMan.hurtWithCleanWater
func (m *Mob) HurtByWaterSplash(fromX, fromZ float64, ownerID int32, ownerIsPlayer, creative bool) (landed, softened bool) {
	if !m.kind().waterSensitive || m.Dead || m.InvulnerableTo(rules.DamageTypeIndirectMagic, creative) {
		return false, false
	}
	landed, softened = m.hurtByDamage(waterSplashDamage, rules.DamageTypeIndirectMagic, ownerID, ownerIsPlayer, creative,
		fromX-m.X, fromZ-m.Z)
	// The teleport tells of itself through the enderman's hurt events; only
	// real damage is landed.
	m.dodgesProjectile(rules.DamageTypeIndirectMagic, creative)
	return landed, softened
}

// waterSplashDamage is the blow of a water bottle on a water-sensitive body.
// vanilla: AbstractThrownPotion.onHitAsWater
const waterSplashDamage = 1.0

// snowGolemStep is the snow golem's end of aiStep: melting where
// snow_golem_melts holds, and a snow trail at the four corners of its feet,
// requested here and placed by the server if mob_griefing allows.
// vanilla: LivingEntity.aiStep, SnowGolem.aiStep
func (m *Mob) snowGolemStep(w World, events []MobEvent) []MobEvent {
	if m.Dead {
		return events
	}
	c := m.cell()
	if w.SnowGolemMelts(c.X, c.Y, c.Z) {
		events = m.bite(rules.DamageTypeOnFire, events)
	}
	if !w.MobGriefing() {
		return events
	}
	y := int32(math.Floor(m.Y))
	for i := range 4 {
		x := int32(math.Floor(m.X + float64(float32(i%2*2-1)*0.25)))
		z := int32(math.Floor(m.Z + float64(float32(i/2%2*2-1)*0.25)))
		if !w.IsAirAt(x, y, z) || !w.SnowLayerSurvives(x, y, z) {
			continue
		}
		events = append(events, MobEvent{Mob: m, Kind: MobLaidSnow, X: float64(x), Y: float64(y), Z: float64(z)})
	}
	return events
}

// bite lands a point of damage with nobody behind it.
func (m *Mob) bite(damageType registry.DamageType, events []MobEvent) []MobEvent {
	if landed, soft := m.plainHurt(1, damageType, false); landed {
		m.noteLastDamage(damageType)
		if !soft {
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: damageType})
		}
	}
	return events
}

// The snow golem's RangedAttackGoal(1.25, 20, 10) and Snowball shot.
// vanilla: SnowGolem.registerGoals, SnowGolem.performRangedAttack
const (
	snowballSpeed    = 1.25
	snowballInterval = 20
	snowballRadius   = 10.0
	// snowballScanInterval is NearestAttackableTargetGoal's randomInterval 10.
	snowballScanInterval = 10
	snowballPower        = 1.6
	snowballInaccuracy   = 12
	// snowballEyeDrop is the 1.1f below the target's eye the shot aims at.
	snowballEyeDrop = float64(float32(1.1))
	// snowballStartDrop is the 0.1f below the golem's eye the snowball starts at.
	snowballStartDrop = float64(float32(0.1))
	snowballLift      = float64(float32(0.2))
)

// throwSnowball is SnowGolem.performRangedAttack.
func (m *Mob) throwSnowball(t *tickView, p *Player, _ float64) {
	_, eye := p.targetBody(t)
	y := m.Y + m.eyeHeight() - snowballStartDrop
	dx, dz := p.X-m.X, p.Z-m.Z
	dy := p.Y + eye - snowballEyeDrop + math.Sqrt(dx*dx+dz*dz)*snowballLift - y
	vx, vy, vz := ShootVelocity(dx, dy, dz, snowballPower, snowballInaccuracy, m.Rand)
	pitch := float32(0.4 / (m.Rand.Float64()*0.4 + 0.8))
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobThrewSnowball, X: m.X, Y: y, Z: m.Z, VX: vx, VY: vy, VZ: vz, Pitch: pitch})
}

// golemCrackLevel is Crackiness.GOLEM.byFraction for an iron golem: 0 none, 1
// low, 2 medium, 3 high; other kinds have none.
// vanilla: Crackiness.GOLEM
func (m *Mob) golemCrackLevel() int {
	if m.Type != TypeIronGolem {
		return 0
	}
	switch fraction := float32(m.Health) / float32(m.healthCap()); {
	case fraction < 0.25:
		return 3
	case fraction < 0.5:
		return 2
	case fraction < 0.75:
		return 1
	}
	return 0
}

// HealIronGolem is IronGolem.mobInteract's iron ingot: 25 health, reporting
// whether the health changed.
func (m *Mob) HealIronGolem() bool {
	before := m.Health
	m.heal(25)
	return m.Health != before
}

// golemCrackStep turns the crack level a blow crossed into its event.
func (m *Mob) golemCrackStep(events []MobEvent) []MobEvent {
	if !m.golemCracked {
		return events
	}
	m.golemCracked = false
	return append(events, MobEvent{Mob: m, Kind: MobGolemCracked})
}
