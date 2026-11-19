package entity

import (
	"cmp"
	"math"
)

// The spider (hunts only in the dark, climbs walls it presses against) and
// the creeper (stops and swells near its target, explodes when the fuse
// runs out), beyond the shared hostile machinery.

// lightDependentMagicValue is Entity.getLightLevelDependentMagicValue at the
// eye cell: LevelReader.getMaxLocalRawBrightness / 15 through the brightness
// curve. The overworld's ambient 0.0 leaves the curve alone, and the
// nether's 0.1 moves no raw light across the spider goals' 0.5. An unloaded
// chunk reads 0, matching vanilla.
func (m *Mob) lightDependentMagicValue(w World) float64 {
	return cellLightDependentMagicValue(w, m.cell().X, int32(math.Floor(m.Y+m.eyeHeight())), m.cell().Z)
}

// cellLightDependentMagicValue is LevelReader.getLightLevelDependentMagicValue
// at a cell (the spider's eye cell, or the sun goals' cells).
func cellLightDependentMagicValue(w World, x, y, z int32) float64 {
	return float64(lightMagicValue(w.MaxLocalRawLightAt(x, y, z)))
}

// lightMagicValue is the curve f / (4 - 3f) in vanilla's float32: rounding
// puts raw light 12 a hair past 0.5, where the exact value sits on it.
func lightMagicValue(raw uint8) float32 {
	f := float32(raw) / 15
	return f / (4 - 3*f)
}

// spiderLightHalf is the 0.5 of SpiderTargetGoal.canUse and
// SpiderAttackGoal.canContinueToUse.
const spiderLightHalf = 0.5

// spiderTargetGoal is Spider$SpiderTargetGoal: the shared
// NearestAttackableTargetGoal, but a spider in light of 0.5 or more acquires
// no target. HurtByTargetGoal is ungated, so a struck spider still fights at
// noon.
type spiderTargetGoal struct {
	nearestTargetGoal
}

func (g *spiderTargetGoal) CanStart(t *tickView) bool {
	if g.m.lightDependentMagicValue(t.w) >= spiderLightHalf {
		return false
	}
	return g.nearestTargetGoal.CanStart(t)
}

// spiderGolemTargetGoal is Spider$SpiderTargetGoal<IronGolem>: the same light
// gate as the player goal.
type spiderGolemTargetGoal struct{ mobTargetGoal }

func (g *spiderGolemTargetGoal) CanStart(t *tickView) bool {
	if g.m.lightDependentMagicValue(t.w) >= spiderLightHalf {
		return false
	}
	return g.mobTargetGoal.CanStart(t)
}

// spiderAttackGoal is Spider$SpiderAttackGoal: the shared melee attack, but
// in light of 0.5 or more the hunt is dropped one tick in 100
// (nextInt(100) == 0). A spider with a rider does not attack; the rider
// controls the pair.
type spiderAttackGoal struct {
	meleeAttackGoal
}

func (g *spiderAttackGoal) CanStart(t *tickView) bool {
	if len(g.m.Passengers) > 0 {
		return false
	}
	return g.meleeAttackGoal.CanStart(t)
}

func (g *spiderAttackGoal) ShouldContinue(t *tickView) bool {
	if len(g.m.Passengers) > 0 {
		return false
	}
	if g.m.lightDependentMagicValue(t.w) >= spiderLightHalf && g.m.Rand.Intn(100) == 0 {
		g.m.SetMetadataTargetID(0)
		return false
	}
	return g.meleeAttackGoal.ShouldContinue(t)
}

// The creeper's swell: SwellGoal holds the legs, and Creeper.tick (before
// the goals) winds the fuse by the swell direction and explodes at max.

// Creeper numbers from the bytecode: a 30-tick fuse and radius 3 from
// the constructor (maxSwell and explosionRadius; Level.explode gets their
// product with the charged doubling), and SwellGoal's distances: canUse
// needs distanceToSqr under 9, and tick deflates beyond 49 (3 and 7
// blocks).
const (
	creeperMaxSwell        = 30
	creeperExplosionRadius = 3
	creeperSwellStartSq    = 9.0
	creeperSwellStopSq     = 49.0
)

// swellGoal is SwellGoal: the fuse runs while the target is within three
// blocks and in sight; a target gone, dead, beyond seven or behind a wall
// deflates it. It holds MOVE, so a swelling creeper stands still.
type swellGoal struct {
	m *Mob
	// targetID is the entity taken at start (SwellGoal.start's field), kept
	// through any retargeting meanwhile.
	targetID int32
}

func (g *swellGoal) Priority() int    { return 2 }
func (g *swellGoal) Flags() GoalFlags { return GoalMove }

// CanStart is SwellGoal.canUse: a rising fuse, or a live target within
// three blocks.
func (g *swellGoal) CanStart(t *tickView) bool {
	if g.m.SwellDir > 0 {
		return true
	}
	p := g.m.targetIn(t)
	return p != nil && Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) < creeperSwellStartSq
}

func (g *swellGoal) Start(t *tickView) {
	// vanilla: SwellGoal.start
	g.m.stopSeeking()
	g.targetID = g.m.TargetID
}

// ShouldContinue is Goal's default canContinueToUse: canUse again.
func (g *swellGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t)
}

func (g *swellGoal) Stop(*tickView) { g.targetID = 0 }

// RequiresEveryTick is SwellGoal's override: the direction is set every
// tick.
func (g *swellGoal) RequiresEveryTick() bool { return true }

// Tick is SwellGoal.tick: deflate for a target gone, dead, beyond seven
// blocks or out of sight, else run. An ignited fuse runs to the blast
// regardless.
func (g *swellGoal) Tick(t *tickView) {
	if g.m.Ignited {
		g.m.SetMetadataSwellDir(1)
		return
	}
	p := g.m.targetByID(t, g.targetID)
	if p == nil {
		g.m.SetMetadataSwellDir(-1)
		return
	}
	if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) > creeperSwellStopSq {
		g.m.SetMetadataSwellDir(-1)
		return
	}
	if !g.m.seesTarget(t.w, p) {
		g.m.SetMetadataSwellDir(-1)
		return
	}
	g.m.SetMetadataSwellDir(1)
}

// targetByID finds a body in the tick's list by entity ID, huntable or not:
// SwellGoal.tick reads its stored entity, not the mob's validated target.
// Mob targets are returned in the same shape as players.
func (m *Mob) targetByID(t *tickView, id int32) *Player {
	if id == 0 {
		return nil
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == id {
			return p
		}
	}
	if o := t.mobByID(id); o != nil && o.Alive() {
		p := o.asTarget()
		return &p
	}
	return nil
}

// Ignite lights the creeper's fuse, which runs to the blast whatever the
// swell goal asks.
// vanilla: Creeper.ignite
func (m *Mob) Ignite() {
	m.SetMetadataIgnited(true)
	m.SetMetadataSwellDir(1)
}

// fuseStep is the creeper's or the sulfur cube's own tick before its goals.
func (m *Mob) fuseStep(w World, events []MobEvent) ([]MobEvent, bool) {
	if m.Type == TypeSulfurCube {
		return m.sulfurFuseStep(w, events)
	}
	return m.swellStep(events)
}

// swellStep is Creeper.tick's fuse: wind by the swell direction (floored at
// 0), play the priming sound when it starts from zero, and at max explode
// (an event with the blast's position and radius) and discard the body, as
// explodeCreeper does. It reports whether the tick ended here.
func (m *Mob) swellStep(events []MobEvent) ([]MobEvent, bool) {
	if m.SwellDir > 0 && m.swell == 0 {
		// vanilla: Creeper.tick's CREEPER_PRIMED
		events = append(events, MobEvent{Mob: m, Kind: MobPrimed})
	}
	m.swell += m.SwellDir
	if m.swell < 0 {
		m.swell = 0
	}
	if m.swell < m.MaxSwell {
		return events, false
	}
	m.swell = m.MaxSwell
	// vanilla: explodeCreeper, the charge doubling the blast.
	radius := float64(m.ExplosionRadius)
	if m.Powered {
		radius *= 2
	}
	events = append(events, MobEvent{
		Mob: m, Kind: MobExploded,
		X: m.X, Y: m.Y, Z: m.Z, Radius: radius,
	})
	// vanilla: explodeCreeper marks the creeper dead and discards it: no
	// death animation, sound or drops.
	m.gone = true
	return events, true
}

// LeapAtTargetGoal's numbers: the squared-distance band (canUse refuses under
// 4.0 and over 16.0, i.e. 2 to 4 blocks), the spider's upward speed 0.4f,
// and the start's horizontal push.
const (
	leapMinDistSq = 4.0
	leapMaxDistSq = 16.0
	leapUpward    = 0.4
	leapForward   = 0.4
)

// leapAtTargetGoal is LeapAtTargetGoal: a grounded mob jumps at a target two
// to four blocks away, one tick in three on average.
type leapAtTargetGoal struct {
	m        *Mob
	priority int
	// yd replaces leapUpward where nonzero: the cat's and ocelot's 0.3.
	yd float64
	// tx, tz are the target's position at the start.
	tx, tz float64
}

func (g *leapAtTargetGoal) Priority() int    { return g.priority }
func (g *leapAtTargetGoal) Flags() GoalFlags { return GoalJump | GoalMove }

func (g *leapAtTargetGoal) CanStart(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil {
		return false
	}
	d := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z)
	if d < leapMinDistSq || d > leapMaxDistSq || !g.m.OnGround {
		return false
	}
	if g.m.Rand.Intn(reducedTickDelay(5)) != 0 {
		return false
	}
	g.tx, g.tz = p.X, p.Z
	return true
}

func (g *leapAtTargetGoal) ShouldContinue(*tickView) bool { return !g.m.OnGround }

// Start is LeapAtTargetGoal.start: a horizontal push of 0.4 toward the
// target plus a fifth of the current motion, and the upward speed.
func (g *leapAtTargetGoal) Start(*tickView) {
	dx, dz := g.tx-g.m.X, g.tz-g.m.Z
	if l2 := dx*dx + dz*dz; l2 > 1e-7 {
		l := math.Sqrt(l2)
		g.m.VX = dx/l*leapForward + g.m.VX*0.2
		g.m.VZ = dz/l*leapForward + g.m.VZ*0.2
	}
	g.m.VY = cmp.Or(g.yd, leapUpward)
	g.m.OnGround = false
}

func (g *leapAtTargetGoal) Tick(*tickView)          {}
func (g *leapAtTargetGoal) Stop(*tickView)          {}
func (g *leapAtTargetGoal) RequiresEveryTick() bool { return false }
