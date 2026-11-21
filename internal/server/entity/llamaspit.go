package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// The llama's spit.
// vanilla: LlamaSpit, Llama.spit, RangedAttackGoal
const (
	// LlamaSpitDamage is the hurt of a spit that lands.
	LlamaSpitDamage  = 1.0
	llamaSpitGravity = 0.06
	// llamaSpitDrag is getAirDrag, a float field.
	llamaSpitDrag = float64(float32(0.99))
	// llamaSpitPower and llamaSpitInaccuracy are spawnProjectileUsingShoot's.
	llamaSpitPower      = 1.5
	llamaSpitInaccuracy = 10
	// llamaSpitInterval is RangedAttackGoal's attack interval, min and max alike.
	llamaSpitInterval = 40
	// llamaSpitRadius is the goal's attack radius.
	llamaSpitRadius = 20.0
	llamaSpitSpeed  = 1.25
	// llamaSpitAimHeight is the share of the target's height aimed at.
	llamaSpitAimHeight = 0.3333333333333333
	// llamaSpitLift is the arc's compensation per block of distance, 0.2f.
	llamaSpitLift = float64(float32(0.2))
	// llamaWolfFollow scales the follow range of the wolf hunt.
	llamaWolfFollow = 0.25
	// llamaSpitPitchSpread is the sound pitch's deviation.
	llamaSpitPitchSpread = 0.2
)

// isWildWolf is LlamaAttackWolfGoal's selector: a wolf nobody has tamed.
func isWildWolf(o *Mob) bool { return o.Type == TypeWolf && !o.Tamable.Tame }

// takeDidSpit reads and clears didSpit.
// vanilla: Llama.LlamaHurtByTargetGoal.canContinueToUse
func (m *Mob) takeDidSpit() bool {
	spat := m.Horse.didSpit
	m.Horse.didSpit = false
	return spat
}

// stepSpit is LlamaSpit.tick: the flight's hit is taken over the whole move,
// a body hit hurts and the spit flies on, a block ends it; then it drags,
// falls, and is discarded when its box holds no air or it is in water.
func (t *Thrown) stepSpit(w World, owner *ArrowTarget, targets []ArrowTarget) []ThrownEvent {
	// vanilla: LlamaSpit.tick, super.tick ahead of the flight
	events := t.touchWater(w)
	clip := clipProjectileFlight(w, t.X, t.Y, t.Z, t.VX, t.VY, t.VZ)
	hitEndX, hitEndY, hitEndZ := clip.end(t.X, t.Y, t.Z, t.VX, t.VY, t.VZ)
	ownerVehicle := int32(0)
	if owner != nil {
		ownerVehicle = owner.RootVehicle
	}
	hit, hitX, hitY, hitZ := sweepThrownTargets(t, targets, thrownHitMargin(t.TickCount), ownerVehicle,
		t.X, t.Y, t.Z, hitEndX, hitEndY, hitEndZ)
	// A deflection turns the velocity, not this tick's move.
	mx, my, mz := t.VX, t.VY, t.VZ
	switch {
	case hit != nil && deflectsProjectile(hit, false):
		if hit.EntityID != t.lastDeflectedBy {
			t.lastDeflectedBy = hit.EntityID
			reverseDeflection(&t.VX, &t.VY, &t.VZ, &t.Yaw, t.Rand, 1)
			events = append(events, ThrownEvent{Kind: ThrownDeflected, Target: *hit, X: hitX, Y: hitY, Z: hitZ})
		}
	case hit != nil:
		events = append(events, ThrownEvent{Kind: ThrownHitTarget, Target: *hit, Damage: LlamaSpitDamage, X: hitX, Y: hitY, Z: hitZ})
	case clip.ok:
		t.Dead = true
		return append(events, ThrownEvent{Kind: ThrownBreak, X: hitEndX, Y: hitEndY, Z: hitEndZ,
			BlockHit: BlockHit{X: clip.bx, Y: clip.by, Z: clip.bz, Face: clip.face,
				HitX: hitEndX, HitY: hitEndY, HitZ: hitEndZ, State: clip.state, Border: clip.border}})
	}

	// vanilla: Projectile.checkLeftOwner, settled at the box the flight started in
	if !t.leftOwner {
		if owner == nil {
			t.leftOwner = true
		} else {
			box := t.thrownBox()
			t.leftOwner = !ownerGroupTouching(&box, owner, ownerVehicle, targets)
		}
	}

	t.Yaw = lerpRotation(t.Yaw, arrowTargetYaw(t.VX, t.VZ))
	t.Pitch = lerpRotation(t.Pitch, arrowTargetPitch(t.VY, t.VX, t.VZ))
	if spitBoxSolidOrWet(w, t) {
		t.Dead = true
		return events
	}
	t.SetPos(t.X+mx, t.Y+my, t.Z+mz)
	t.VX *= llamaSpitDrag
	t.VY = t.VY*llamaSpitDrag - llamaSpitGravity
	t.VZ *= llamaSpitDrag
	return events
}

// spitBoxSolidOrWet reports a box with no air block in it, or a body in
// water. vanilla: LlamaSpit.tick
func spitBoxSolidOrWet(w World, t *Thrown) bool {
	x0, x1 := int32(math.Floor(t.X-ThrownHalfSize)), int32(math.Floor(t.X+ThrownHalfSize))
	y0, y1 := int32(math.Floor(t.Y)), int32(math.Floor(t.Y+ThrownHeight))
	z0, z1 := int32(math.Floor(t.Z-ThrownHalfSize)), int32(math.Floor(t.Z+ThrownHalfSize))
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				if world.IsAirState(w.BlockAt(x, y, z)) {
					return rules.IsWater(w.BlockAt(int32(math.Floor(t.X)), int32(math.Floor(t.Y)), int32(math.Floor(t.Z))))
				}
			}
		}
	}
	return true
}

// rangedGoal is RangedAttackGoal(speed, interval, radius): close in until the
// target is within radius blocks and in sight for five ticks, then fire every
// interval ticks.
type rangedGoal struct {
	m        *Mob
	priority int // zero means priorityPanic
	speed    float64
	interval int
	radius   float64
	// fire is performRangedAttack, given the target and its height.
	fire       func(t *tickView, p *Player, height float64)
	seeTime    int
	attackTime int
}

// newSpitGoal is the llama's RangedAttackGoal(1.25, 40, 20) at priority 3.
func newSpitGoal(m *Mob) *rangedGoal {
	return &rangedGoal{m: m, priority: 3, speed: llamaSpitSpeed, interval: llamaSpitInterval, radius: llamaSpitRadius, fire: m.spit}
}

func (g *rangedGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityPanic
}
func (g *rangedGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *rangedGoal) CanStart(t *tickView) bool { return g.m.targetIn(t) != nil }

func (g *rangedGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t) || g.m.TargetID != 0 && !g.m.navDone()
}

func (g *rangedGoal) Start(*tickView) { g.attackTime = -1 }

func (g *rangedGoal) Stop(*tickView) {
	g.seeTime = 0
	g.attackTime = -1
}

func (g *rangedGoal) RequiresEveryTick() bool { return true }

func (g *rangedGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	height, eye := p.targetBody(t)
	distSq := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	seen := m.seesPoint(t.w, p.X, p.Y+eye, p.Z)
	if seen {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	if distSq > g.radius*g.radius || g.seeTime < 5 {
		m.seekTo(t.w, p.X, p.Y, p.Z, g.speed, navReach)
	} else {
		m.stopSeeking()
	}
	m.lookAtPoint(p.X, p.Y+eye, p.Z)
	g.attackTime--
	switch {
	case g.attackTime == 0:
		if !seen {
			return
		}
		g.fire(t, p, height)
		g.attackTime = g.interval
	case g.attackTime < 0:
		g.attackTime = g.interval
	}
}

// targetBody is the target's height and eye height above its feet.
func (p *Player) targetBody(t *tickView) (height, eye float64) {
	if o := t.mobByID(p.EntityID); o != nil {
		return o.height(), o.eyeHeight()
	}
	_, height, eye = p.box()
	return height, eye
}

// spit is Llama.spit: the spit starts beside the llama along its body and is
// aimed a third up the target, lifted a fifth of the distance, with the
// shoot's noise drawn from the llama's own source.
func (m *Mob) spit(t *tickView, p *Player, targetHeight float64) {
	rad := float64(m.Yaw) * math.Pi / 180
	reach := (m.width() + 1) * 0.5
	x := m.X - reach*math.Sin(rad)
	y := m.Y + m.eyeHeight() - float64(float32(0.1))
	z := m.Z + reach*math.Cos(rad)
	dx, dz := p.X-m.X, p.Z-m.Z
	dy := p.Y + targetHeight*llamaSpitAimHeight - y + math.Sqrt(dx*dx+dz*dz)*llamaSpitLift
	vx, vy, vz := ShootVelocity(dx, dy, dz, llamaSpitPower, llamaSpitInaccuracy, m.Rand)
	pitch := float32(1 + (m.Rand.Float64()-m.Rand.Float64())*llamaSpitPitchSpread)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSpat, X: x, Y: y, Z: z, VX: vx, VY: vy, VZ: vz, Pitch: pitch})
	m.Horse.didSpit = true
}
