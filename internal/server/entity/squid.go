package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The squid and glow squid: they move only by Squid.aiStep's tentacle pump,
// fed by the movement vector their own two goals set; no path, no pace. A
// hurt squirts ink and sends the body away from the attacker, and dims a
// glow squid for 100 ticks.

// Squid's tentacle numbers: the swim vector's horizontal and vertical
// reach, the goal's 25-tick rhythm, the 100 idle ticks that still it, and
// the flee's push of 3, tapering from five blocks out.
const (
	squidSwimArm   = 0.2
	squidSwimDip   = 0.1
	squidRollEvery = 25
	squidIdleAfter = 100
	squidFleeSpeed = 3.0
)

// squidMoveStep is the squid's goals, aiStep and travel in one, instead of
// walk physics: Squid registers only its two goals, uses no navigation, and
// replaces travel with a plain move. The goals set the swim vector, travel
// carries the last pump's velocity (super.aiStep), and the tentacle cycle
// sets the next; its wrap broadcasts the client's animation restart, and the
// glow squid's dark ticks count down.
// vanilla: Squid.aiStep, Squid.travel, GlowSquid.aiStep
func (m *Mob) squidMoveStep(t *tickView) []MobEvent {
	// The random goal turns the vector every other tick; the flee overrides
	// it while a fresh hurt by someone stands and the body is in water within
	// ten blocks of them (SquidFleeGoal.canUse, asked every tick). Neither
	// goal stops the other, so wandering resumes as soon as the flee lapses.
	m.squidWander()
	if attacker := m.squidAttacker(t); attacker != nil &&
		m.InWater() && Dist2(m.X, m.Y, m.Z, attacker.X, attacker.Y, attacker.Z) < 100 {
		m.squidFleeFrom(t, attacker)
	}

	// Then the body travels, on the velocity the last pump left; the pump
	// below aims the next tick's.
	events := m.squidTravel(t)

	m.squid.tentacle += m.squid.pace
	if m.squid.tentacle > math.Pi*2 {
		m.squid.tentacle -= math.Pi * 2
		if m.Rand.Intn(10) == 0 {
			// vanilla: the constructor's own re-roll, by the tenth
			m.squid.pace = squidSwimArm / (1 + m.Rand.Float64())
		}
		events = append(events, MobEvent{Mob: m, Kind: MobTentacleReset})
	}
	if m.DarkTicks > 0 {
		m.SetMetadataDarkTicks(m.DarkTicks - 1)
	}

	if m.InWater() {
		if m.squid.tentacle < math.Pi {
			if m.squid.tentacle/math.Pi > 0.75 {
				m.VX, m.VY, m.VZ = m.squid.moveX, m.squid.moveY, m.squid.moveZ
			}
		} else {
			m.VX, m.VY, m.VZ = m.VX*0.9, m.VY*0.9, m.VZ*0.9
		}
		// The body faces its motion, the eased body rotation vanilla's
		// yBodyRot keeps.
		m.Yaw = easeYaw(m.Yaw, float32(yawToward(m.VX, m.VZ)))
	}
	return events
}

// squidTravel is Squid.travel on the last pump's velocity: in water a plain
// move; beached, only the vertical falls under gravity and air drag, and
// the landing banks and hurts like walk physics.
// vanilla: Squid.travel, Squid.aiStep, Entity.move, checkFallDamage
func (m *Mob) squidTravel(t *tickView) []MobEvent {
	if m.InWater() {
		m.move(t.w, m.VX, m.VY, m.VZ)
		return nil
	}
	wasOnGround := m.OnGround
	oldY := m.Y
	m.VX, m.VZ = 0, 0
	if lift, ok := rules.LevitationLift(m.effects.All()); ok {
		m.VY = lift * verticalDrag
	} else {
		m.VY = (m.VY - m.gravity()) * verticalDrag
	}
	m.move(t.w, 0, m.VY, 0)
	if dy := m.Y - oldY; dy > physicsEpsilon {
		m.FallDistance = 0
	} else if dy < -physicsEpsilon {
		m.FallDistance += -dy
	}
	if !m.justLanded {
		return nil
	}
	landed := m.FallDistance
	m.LandedFall = landed
	m.FallDistance = 0
	if wasOnGround || m.fallDamageImmune() {
		return nil
	}
	d := rules.FallDamage(landed, rules.SafeFallDistance(m.effects.All()), 1)
	if d <= 0 {
		return nil
	}
	if hurt, soft := m.plainHurt(d, rules.DamageTypeFall, false); hurt {
		m.noteFallHurt(rules.DamageTypeFall, landed)
		return []MobEvent{{
			Mob: m, Kind: MobLanded, Softened: soft,
			Damage: d, DamageType: rules.DamageTypeFall,
		}}
	}
	return nil
}

// easeYaw is Squid.aiStep's body rotation: a tenth of the remaining gap to
// the movement direction each tick, never quite snapping onto it.
// vanilla: Squid.aiStep
func easeYaw(current, target float32) float32 {
	delta := math.Mod(float64(target-current)+540, 360) - 180
	return current + float32(delta*0.1)
}

// squidWander is SquidRandomMovementGoal: a fresh vector on a 1-in-25 roll,
// when out of water, or when the last vector has nearly run out, rolled at
// the selector's every-other-tick cadence. Past 100 idle ticks the vector
// is zeroed instead.
func (m *Mob) squidWander() {
	if (m.Age+int(m.ID))%2 != 0 && m.Age > 1 {
		return
	}
	if m.NoActionTicks > squidIdleAfter {
		m.squid.moveX, m.squid.moveY, m.squid.moveZ = 0, 0, 0
		return
	}
	if m.Rand.Intn(squidRollEvery) != 0 && m.InWater() && m.hasSquidVector() {
		return
	}
	angle := m.Rand.Float64() * math.Pi * 2
	m.squid.moveX = math.Cos(angle) * squidSwimArm
	m.squid.moveY = -squidSwimDip + m.Rand.Float64()*squidSwimDip*2
	m.squid.moveZ = math.Sin(angle) * squidSwimArm
}

// hasSquidVector is Squid.hasMovementVector: a vector worth keeping.
func (m *Mob) hasSquidVector() bool {
	return m.squid.moveX*m.squid.moveX+m.squid.moveY*m.squid.moveY+m.squid.moveZ*m.squid.moveZ > 1.0e-5
}

// squidAttacker is the getLastHurtByMob the flee answers: the last attacker
// while vanilla's 100-tick memory of it lasts.
// vanilla: LivingEntity.setLastHurtByMob, Squid$SquidFleeGoal.canUse
func (m *Mob) squidAttacker(t *tickView) *Player {
	if m.playerMemoryTicks > 0 && m.hurtByPlayer {
		for i := range t.players {
			p := &t.players[i]
			if p.EntityID == m.retaliate {
				return p
			}
		}
		return nil // the attacker is gone from the world
	}
	if m.retaliate == 0 { // no entity has ID 0: nobody struck the body
		return nil
	}
	for _, other := range t.others {
		if other != nil && other.ID == m.retaliate {
			return &Player{X: other.X, Y: other.Y, Z: other.Z, EntityID: other.ID}
		}
	}
	return nil
}

// squidFleeFrom is Squid$SquidFleeGoal: the vector away from the attacker,
// the raw offset scaled by a push of 3 within five blocks tapering to zero
// at ten (the goal's normalize result is discarded, the vector being
// immutable in vanilla); the vertical is dropped when the cell ahead is air,
// and the old vector kept when it is neither water nor air.
// vanilla: Squid$SquidFleeGoal.tick
func (m *Mob) squidFleeFrom(t *tickView, attacker *Player) {
	fleeX, fleeY, fleeZ := m.X-attacker.X, m.Y-attacker.Y, m.Z-attacker.Z
	ahead := t.w.BlockAt(floorI(m.X+fleeX), floorI(m.Y+fleeY), floorI(m.Z+fleeZ))
	if !rules.IsWater(ahead) && registry.BlockOf(ahead) != registry.BlockAir {
		// Neither water nor air ahead: the last vector stays.
		return
	}
	if length := math.Sqrt(fleeX*fleeX + fleeY*fleeY + fleeZ*fleeZ); length > 0 {
		push := squidFleeSpeed
		if length > 5 {
			push -= (length - 5) / 5
		}
		if push > 0 {
			fleeX, fleeY, fleeZ = fleeX*push, fleeY*push, fleeZ*push
		}
	}
	if registry.BlockOf(ahead) == registry.BlockAir {
		fleeY = 0
	}
	m.squid.moveX, m.squid.moveY, m.squid.moveZ = fleeX/20, fleeY/20, fleeZ/20
}

// noteSquidHurt is the squid's half of a landed hurt: ink whenever an
// attacker is behind it (just set, or remembered), and 100 dark ticks for a
// glow squid on any landed hurt.
// vanilla: Squid.hurtServer, GlowSquid.hurtServer, Squid.spawnInk
func (m *Mob) noteSquidHurt() {
	m.noteGuardianHurt()
	m.noteShulkerHurt()
	m.noteHorseHurt()
	if !m.is(TypeSquid) {
		return
	}
	if m.Type == TypeGlowSquid {
		m.SetMetadataDarkTicks(100)
	}
	if m.retaliate != 0 {
		m.Squirt = true
	}
}

// squidSpawnPositionOk is IN_WATER placement plus the squids' spawn rules:
// no redstone conductor above, and water at the cell (the water block for
// the underground category, any water fluid for the surface one). The
// surface squid also needs height in [sea-13, sea] with the water block
// above and water fluid below; the glow squid needs to be 33 or more below
// sea level, in the dark.
// vanilla: SpawnPlacementTypes.IN_WATER,
// AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules,
// GlowSquid.checkGlowSquidSpawnRules
func squidSpawnPositionOk(w SpawnWorld, category SpawnCategory, x, y, z int32) bool {
	if conductorAt(w, x, y+1, z) {
		return false
	}
	if category == CategoryUndergroundWater {
		if !rules.IsWaterBlock(w.BlockAt(x, y, z)) {
			return false
		}
		return y <= w.SeaLevel()-33 &&
			w.BlockLightAt(x, y, z) == 0 && w.SkyLightAt(x, y, z) == 0
	}
	if !rules.IsWater(w.BlockAt(x, y, z)) {
		return false
	}
	sea := w.SeaLevel()
	return y >= sea-13 && y <= sea &&
		rules.IsWaterBlock(w.BlockAt(x, y+1, z)) && rules.IsWater(w.BlockAt(x, y-1, z))
}

// floorI is the block coordinate of a world position.
func floorI(v float64) int32 { return int32(math.Floor(v)) }
