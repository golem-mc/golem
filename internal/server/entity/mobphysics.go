package entity

import (
	"cmp"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Living-body physics numbers: gravity 0.08 with 0.02 air drag on the fall;
// horizontal air drag near a tenth; on the ground the block's slipperiness
// (0.6 for ordinary blocks) times the air's after the move. Water is
// travelInWater's: drag 0.8 (getWaterSlowDown), acceleration 0.02, a
// sixteenth off the fall after the move. A jump starts at 0.42
// (JUMP_STRENGTH); the first three blocks of a fall are free; a 0.6 ledge is
// stepped up without a jump. The flying speed is the attribute's 0.02.
const (
	mobGravity           = 0.08
	verticalDrag         = 0.98
	airDrag              = 0.91
	slipperiness         = 0.6
	flyingSpeed          = 0.02
	defaultWaterSlowDown = 0.8
	waterSpeed           = 0.02
	stepHeight           = 0.6
	chickenFlap          = 0.6

	// climbSpeed is handleOnClimbable's 0.15 clamp; climbUp is the 0.2 a climb
	// gives the next move against a wall or when jumping.
	climbSpeed = 0.15
	climbUp    = 0.2

	// physicsEpsilon shaves collision boxes so a box exactly on a face reads as
	// touching, not inside.
	physicsEpsilon = 1e-7
)

// powderSnowState is the state whose collision depends on the body, see
// powderSnowBoxes.
var powderSnowState = registry.MustState("minecraft:powder_snow", nil)

// cellExtent is blockExtent for the cell, reading a moving piston's ride
// instead of its state.
func cellExtent(w World, state uint16, x, y, z int32) float64 {
	if world.IsShulkerBoxShape(state) {
		top := 0.0
		for _, b := range w.ShulkerBoxBoxes(state, x, y, z) {
			top = max(top, b.MaxY)
		}
		return top
	}
	if !world.IsMovingPiston(state) {
		return blockExtent(state)
	}
	top := 0.0
	for _, b := range w.MovingPistonBoxes(x, y, z) {
		top = max(top, b.MaxY)
	}
	return top
}

// blockExtent is the top of a state's collision boxes above its cell floor.
// vanilla: BlockState.getCollisionShape
func blockExtent(state uint16) float64 {
	return world.CollisionTop(state)
}

// onClimbable is LivingEntity.onClimbable: a ladder, vine, scaffolding, or
// an open trapdoor over a ladder at the feet. The spider uses its climbing
// flag instead.
// vanilla: LivingEntity.onClimbable, Spider.onClimbable
func (m *Mob) onClimbable(w World) bool {
	if m.is(TypeSpider) {
		return m.Climbing
	}
	cell := m.cell()
	state := w.BlockAt(cell.X, cell.Y, cell.Z)
	if !rules.IsClimbable(state, w.BlockAt(cell.X, cell.Y-1, cell.Z)) {
		return false
	}
	// vanilla: LivingEntity.lastClimbablePos
	m.climbedFrom = rules.BlockFallLocation(state)
	return true
}

// gravity is LivingEntity.getGravity, including Entity.isNoGravity.
func (m *Mob) gravity() float64 {
	if m.NoGravity {
		return 0
	}
	return m.attrOr(registry.AttributeGravity, mobGravity)
}

// effectiveGravity is gravity under Slow Falling's 0.01 cap while the body
// is not rising; the air, water and lava travels use it.
// vanilla: LivingEntity.getEffectiveGravity
func (m *Mob) effectiveGravity() float64 {
	g := m.gravity()
	if m.VY <= 0 && rules.HasSlowFalling(m.effects.All()) {
		return math.Min(g, 0.01)
	}
	return g
}

// zeroSmallVelocity is aiStep's opening, run before the NoAI gate so a saved
// Motion does not drift: components under 0.003 become zero.
// vanilla: LivingEntity.aiStep
func (m *Mob) zeroSmallVelocity() {
	if math.Abs(m.VX) < 0.003 {
		m.VX = 0
	}
	if math.Abs(m.VY) < 0.003 {
		m.VY = 0
	}
	if math.Abs(m.VZ) < 0.003 {
		m.VZ = 0
	}
}

// jumpStep is aiStep's jump handling before travel: a requested jump
// becomes the fluid's kick in deep water or lava (lava unless shallow), or a
// ground jump on land or in shallows, at most one per ten ticks. Not asking
// clears the wait. It leaves m.jump set; the climber reads it after the
// move.
func (m *Mob) jumpStep(w World) {
	// The ground jump's breath between hops, vanilla's noJumpDelay.
	if m.noJumpDelay > 0 {
		m.noJumpDelay--
	}
	if m.jump {
		fluidHeight := m.waterFluid.height
		if m.InLava() {
			fluidHeight = m.lavaFluid.height
		}
		inWater := m.InWater() && fluidHeight > 0
		threshold := m.fluidJumpThreshold()
		if inWater && (!m.OnGround || fluidHeight > threshold) {
			m.jumpInLiquid(fluidWater)
		} else if m.InLava() && (!m.OnGround || m.lavaFluid.height > threshold) {
			m.jumpInLiquid(fluidLava)
		} else if (m.OnGround || inWater && fluidHeight <= threshold) && m.noJumpDelay == 0 {
			m.jumpFromGround(blockJumpFactor(w, m.X, m.Y, m.Z))
			m.noJumpDelay = 10
		}
	} else {
		m.noJumpDelay = 0
	}
}

// walksByStep reports whether the body uses the shared walking travel.
func (m *Mob) walksByStep() bool {
	return !m.is(TypeSquid) && !m.IsFish() && !m.IsShulker() && !m.IsGuardian() &&
		m.Type != TypeVex && m.Type != TypeParrot && !m.kind().flying && m.Type != TypeBat && m.Type != TypePhantom
}

// seatedTravel is the air travel of a seated rider with its AI, from the
// zero velocity rideTick left: gravity and drag set its velocity, which the
// mount's splash reads, and the body stays put, as the mount's tick seats it
// again. A rider in fluid, or of a kind that moves by its own step, keeps
// the zero.
// vanilla: Entity.rideTick, LivingEntity.travelInAir
func (m *Mob) seatedTravel() {
	if m.InWater() || m.InLava() || !m.walksByStep() {
		return
	}
	_, drag := m.airDrags()
	if lift, ok := rules.LevitationLift(m.effects.All()); ok {
		m.VY = lift * 0.2 * drag
		return
	}
	m.VY = -m.effectiveGravity() * drag
}

// step moves the body one tick, as LivingEntity.aiStep ends: the jump, then
// travel (the water branch for a body in fluid, the air's otherwise), with
// friction after the move. Landing on a floor spends the fall; rising
// forgets it.
func (m *Mob) step(w World, events []MobEvent) []MobEvent {
	if m.Type == TypeTadpole && !m.InWater() {
		events = m.fishFlop(events)
	}
	wasOnGround := m.OnGround
	oldX, oldY, oldZ := m.X, m.Y, m.Z

	m.jumpStep(w)

	// travelInAir reads block friction once, before the move, for both the
	// acceleration and the drag.
	// vanilla: LivingEntity.travelInAir
	blockFriction := 1.0
	if m.OnGround {
		blockFriction = groundFrictionBelow(w, m.X, m.Y, m.Z, m.width()/2)
		modifier := 1.0
		if m.Type == TypeSulfurCube {
			modifier = m.sulfurProf().friction
		}
		blockFriction = float64(computeModifiedFriction(float32(blockFriction), float32(m.attrOr(registry.AttributeFrictionModifier, modifier))))
	}
	// Slow Falling and Levitation write the fall off before the travel.
	// vanilla: LivingEntity.aiStep
	if rules.ResetsFall(m.effects.All()) {
		m.FallDistance = 0
	}

	// The fluid trackers pick the travel branch (canStandOnFluid excepted);
	// the water and lava branches are in mobfluids.go.
	inFluid := (m.InWater() || m.InLava()) && !m.standsOnFluid(w)
	if inFluid {
		m.travelInFluid(w)
	} else if m.walk.speed > 0 {
		// moveRelative: the wanted direction at the move control's pace, through
		// getFrictionInfluencedSpeed, which leaves it whole on 0.6 friction and
		// divides it down on slipperier ground. Airborne, the flying speed is used.
		speedArg := flyingSpeed
		if m.OnGround {
			speedArg = m.walk.speed
			// vanilla: LivingEntity.getFrictionInfluencedSpeed
			if blockFriction > slipperiness {
				speedArg *= 0.21600002 / (blockFriction * blockFriction * blockFriction)
			}
		}
		pull := speedArg
		if m.walk.speed <= 1 {
			pull *= m.walk.speed
		}
		m.VX += m.walk.dirX * pull
		m.VZ += m.walk.dirZ * pull
	}

	if !inFluid {
		// handleOnClimbable: sideways speed and the fall clamped to ±0.15, and the
		// banked fall cleared before the move, so a body caught by a ladder lands
		// nothing.
		climbing := m.onClimbable(w)
		if climbing {
			m.FallDistance = 0
			m.VX = math.Max(-climbSpeed, math.Min(climbSpeed, m.VX))
			m.VZ = math.Max(-climbSpeed, math.Min(climbSpeed, m.VZ))
			if m.VY < -climbSpeed {
				m.VY = -climbSpeed
			}
		}

		m.move(w, m.VX, m.VY, m.VZ)

		// The climb: against a wall or jumping while climbing (or a walker
		// that was in powder snow), the vertical velocity becomes 0.2 before
		// gravity.
		// vanilla: LivingEntity.handleRelativeFrictionAndCalculateMovement
		if (m.horizontalCollision || m.jump) && (climbing || m.wasInPowderSnow && m.collideCtx().walker) {
			m.VY = climbUp
		}

		// Gravity and drag come off after the move, so a jump's first tick rises
		// the whole 0.42 and its apex clears a block. A no-gravity body keeps the
		// drag; a goat or breeze in a long jump keeps its velocity whole.
		// vanilla: LivingEntity.travelInAir's shouldDiscardFriction
		discard := m.Type == TypeGoat && m.Goat.jump.noFriction || m.Type == TypeFrog && m.frog.jump.noFriction ||
			m.Type == TypeBreeze && m.breeze.noFriction
		if lift, ok := rules.LevitationLift(m.effects.All()); ok {
			m.VY += (lift - m.VY) * 0.2
		} else {
			m.VY -= m.effectiveGravity()
		}
		if !discard {
			horizontal, vertical := m.airDrags()
			friction := float64(float32(blockFriction) * float32(horizontal))
			m.VY *= vertical
			m.VX *= friction
			m.VZ *= friction
		}
	}
	// stepOn follows the whole travel, in fluid too.
	// vanilla: LivingEntity.aiStep's applyEffectsFromBlocks
	if m.OnGround && !m.SteppingCarefully() {
		slimeStep(w, m.X, m.Y, m.Z, m.width()/2, m.VY, &m.VX, &m.VZ)
	}

	// Water is read where the move left the feet, as checkFallDamage runs
	// after the move.
	m.bankFall(w, oldX, oldY, oldZ)
	events = m.honeySlide(w, events)
	if m.bouncedOff {
		events = append(events, MobEvent{Mob: m, Kind: MobBouncedOff})
	}
	if m.bounced {
		x, y, z, state := rules.OnPosCell(w, m.X, m.Y, m.Z, m.width()/2)
		if sound := rules.BounceSound(state); sound != registry.SoundNone {
			events = append(events, MobEvent{Mob: m, Kind: MobBounced, SoundID: sound, X: float64(x), Y: float64(y), Z: float64(z)})
		}
	}
	if m.justLanded {
		// Every landing banks its distance for the server's farmland trample,
		// read after the tick; only a fall from the air hurts.
		landed := m.FallDistance
		events = m.locatedLanding(events, landed)
		m.LandedFall = landed
		m.FallDistance = 0
		// vanilla: HoneyBlock.fallOn, before the fall's blow
		if landed > 0 && rules.IsHoneyBlock(rules.OnPos(w, m.X, m.Y, m.Z, m.BbWidth()/2)) {
			events = append(events,
				MobEvent{Mob: m, Kind: MobSounded, SoundID: honeySlideSound, Volume: 1, Pitch: 1},
				MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityEventHoneySlide})
		}
		if !wasOnGround {
			if m.Type == TypeBreeze && landed > 3 {
				events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityBreezeLand, Volume: 1, Pitch: 1})
			}
			if safe, _ := m.fallParams(); landed > 0 {
				if count := rules.LandingParticleCount(landed, safe); count > 0 {
					ox, _, oz, state := rules.OnPosCell(w, m.X, m.Y, m.Z, m.BbWidth()/2)
					if registry.BlockOf(state) != registry.BlockAir {
						x, z := rules.LandingParticleXZ(m.X, m.Z, ox, oz)
						events = append(events, MobEvent{Mob: m, Kind: MobLandingDust, BlockState: state, Count: count, X: x, Y: m.Y, Z: z})
					}
				}
			}
			d, hurt, soft, effect := m.fallBlow(w, landed)
			// vanilla: Creeper.causeFallDamage
			if m.Type == TypeCreeper && landed > 0 {
				m.swell = min(m.swell+int(landed*1.5), m.MaxSwell-5)
			}
			damageType := effect.DamageType
			// A horse's land sound plays for any fall over a block.
			if hurt || (IsEquine(m.Type) && landed > 1) {
				events = append(events, MobEvent{
					Mob: m, Kind: MobLanded, Softened: soft,
					Damage: d, DamageType: damageType, Fall: landed,
				})
			}
		}
	}

	// vanilla: LivingEntity.checkFallDamage forgets the climb on the ground
	if m.OnGround {
		m.climbedFrom = ""
	}

	// A cobweb, berry bush or powder snow clears the fall after the landing
	// was hurt.
	// vanilla: Entity.makeStuckInBlock
	if m.stuck != (stuckFactor{}) {
		m.FallDistance = 0
	}

	// Chicken.aiStep's flap after travel: a falling chicken keeps 0.6 of its
	// fall each tick.
	if m.Type == TypeChicken && !m.OnGround && m.VY < 0 {
		m.VY *= chickenFlap
	}

	// JumpControl clears the request every tick; kept, it would make a
	// swimmer kick every tick and rise to the surface instead of bobbing.
	m.jump = false
	return events
}

// move advances the mob by the velocity as far as blocks allow, vertical
// axis first, then horizontal with the step-up retry. It leaves
// horizontalCollision set for the spider's climbing.
func (m *Mob) move(w World, dx, dy, dz float64) {
	m.justLanded = false
	m.bounced, m.bouncedOff = false, false
	m.horizontalCollision = false

	// vanilla: Entity.move spends the stuck multiplier the last move left,
	// and the velocity with it.
	var stuck bool
	if dx, dy, dz, stuck = m.stuck.scale(dx, dy, dz); stuck {
		m.VX, m.VY, m.VZ = 0, 0, 0
	}

	cells := gatherMoveCells(w, m.mobBox(), dx, dy, dz, m.stepHeight())
	defer cells.release()
	ctx := m.collideCtx()
	ctx.moveCells = &cells

	if dy < 0 {
		oldY := m.Y
		landY, blocked := clipFall(w, ctx, m.X, m.Z, m.width()/2, m.Y, dy)
		if blocked {
			m.Y = landY
			m.VY = m.bounceOnLanding(w, m.Y-oldY)
			if m.VY > 0 {
				m.Impulse = true
				m.bounced = true
			}
			m.bouncedOff = m.VY > 0
			m.OnGround = true
			m.justLanded = true
		} else {
			m.Y = landY
			m.OnGround = false
		}
	} else if dy > 0 {
		topY, blocked := clipRise(w, ctx, m.X, m.Z, m.width()/2, m.Y+m.height(), dy)
		moved := topY - m.height() - m.Y
		m.Y = topY - m.height()
		if blocked {
			m.VY = m.bounceOffCeiling(moved)
			m.Impulse = m.Impulse || m.VY != 0
			m.bouncedOff = m.bouncedOff || m.VY != 0
		}
		m.OnGround = false
	}

	if dx != 0 || dz != 0 {
		m.moveHorizontal(w, ctx, dx, dz)
	}

	// The block's speed factor scales the horizontal velocity after the move;
	// then the cells the box ended in set the next move's stuck multiplier.
	// vanilla: Entity.move, LivingEntity.getBlockSpeedFactor
	f := blockSpeedFactor(w, m.X, m.Y, m.Z)
	if m.attrs != nil {
		// Mth.lerp(float, float, float): efficiency 1 cancels the slowdown.
		eff := float32(m.attrs.Value(registry.AttributeMovementEfficiency))
		base := float32(f)
		f = float64(base + eff*(1-base))
	}
	m.VX *= f
	m.VZ *= f
	m.stuck = m.stuckInBlocks(w)
}

const honeySlideSound = registry.SoundBlockHoneyBlockSlide

// honeySlide slides the mob down the side of the first honey block in its
// box it presses against, after the travel's gravity and drag; each tick
// the slide may play its sound and send its particle event.
// vanilla: HoneyBlock.entityInside, maybeDoSlideEffects
func (m *Mob) honeySlide(w World, events []MobEvent) []MobEvent {
	if !rules.HoneyFalling(m.VY, m.OnGround) {
		return events // isSlidingDown's cell-free tests, ahead of the cell scan
	}
	slid := false
	m.mobBox().insideCells(func(x, y, z int32) {
		if slid || !rules.IsHoneyBlock(w.BlockAt(x, y, z)) ||
			!rules.HoneySliding(x, y, z, m.X, m.Y, m.Z, m.VY, m.BbWidth(), m.OnGround) {
			return
		}
		slid = true
		m.VX, m.VY, m.VZ = rules.HoneySlide(m.VX, m.VY, m.VZ)
		m.FallDistance = 0
		if m.Rand.Intn(5) == 0 {
			events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: honeySlideSound, Volume: 1, Pitch: 1})
		}
		if m.Rand.Intn(5) == 0 {
			events = append(events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityEventHoneySlideParticles})
		}
	})
	return events
}

// The kinds the berry bush ignores and the effect that loosens a cobweb.
const typeFox = registry.EntityTypeFox

const effectWeaving = registry.MobEffectWeaving

// stuckInBlocks is the stuck multiplier of the cells in the box. A spider
// ignores cobweb; a fox or bee ignores berry bushes; the wither ignores all.
// vanilla: Spider.makeStuckInBlock, SweetBerryBushBlock.entityInside
func (m *Mob) stuckInBlocks(w World) stuckFactor {
	if m.Type == TypeWither {
		return stuckFactor{} // vanilla: WitherBoss.makeStuckInBlock
	}
	_, weaving := m.effects.Get(effectWeaving)
	fam := m.family()
	return stuckIn(w, m.mobBox(), m.X, m.Y, m.Z, true, weaving,
		fam == TypeSpider || fam == TypeCaveSpider, m.Type == typeFox || m.Type == TypeBee)
}

// moveHorizontal moves the box along both horizontal axes, larger first; if
// blocked while on the ground it retries lifted onto the obstacle (the
// step-up). A stopped axis is vanilla's horizontal collision.
func (m *Mob) moveHorizontal(w World, ctx collideCtx, dx, dz float64) {
	h, hw := m.height(), m.width()/2
	startX, startZ := m.X, m.Z

	box, obstacle, blockedX, blockedZ := collideHorizontal(w, ctx, m.mobBox(), dx, dz)
	nextX, nextZ := box.centerXZ()

	if (blockedX || blockedZ) && m.OnGround {
		if climb := obstacle - m.Y; climb > physicsEpsilon && climb <= m.stepHeight() && boxFits(w, ctx, startX, startZ, obstacle, h, hw) {
			lifted := AABB{startX - hw, obstacle, startZ - hw, startX + hw, obstacle + h, startZ + hw}
			rbox, _, _, _ := collideHorizontal(w, ctx, lifted, dx, dz)
			rx, rz := rbox.centerXZ()
			// The lifted move wins if it got further along the asked-for way.
			if math.Hypot(rx-startX, rz-startZ) > math.Hypot(nextX-startX, nextZ-startZ) {
				m.SetPos(rx, obstacle, rz)
				return
			}
		}
	}
	m.SetPos(nextX, m.Y, nextZ)
	m.horizontalCollision = blockedX || blockedZ
	// As in Entity.move, a clipped velocity axis is zeroed rather than
	// carried into the next tick.
	if blockedX {
		m.VX = m.bounceOffWall(m.VX)
		m.bouncedOff = m.bouncedOff || m.VX != 0
	}
	if blockedZ {
		m.VZ = m.bounceOffWall(m.VZ)
		m.bouncedOff = m.bouncedOff || m.VZ != 0
	}
	m.Impulse = m.Impulse || m.bouncedOff || (blockedX || blockedZ) && m.Type == TypeSulfurCube
}

// clipFall clamps a downward move to the highest colliding top under the
// box. Exactly reaching a top counts as stopped, which keeps a standing body
// up.
func clipFall(w World, c collideCtx, x, z, hw, feetY, dy float64) (landY float64, blocked bool) {
	out, _, blocked := clipMove(w, c, AABB{x - hw, feetY, z - hw, x + hw, feetY, z + hw}, axisY, dy)
	return out.minY, blocked
}

// clipRise clamps an upward move so the head stops flush under a ceiling.
func clipRise(w World, c collideCtx, x, z, hw, headY, dy float64) (topY float64, blocked bool) {
	out, _, blocked := clipMove(w, c, AABB{x - hw, headY, z - hw, x + hw, headY, z + hw}, axisY, dy)
	return out.maxY, blocked
}

// boxFits reports whether the box at (x, z) with feet at y overlaps nothing
// solid, the step-up's headroom check.
func boxFits(w World, c collideCtx, x, z, y, h, hw float64) bool {
	return !overlapsSolid(w, c, AABB{x - hw, y, z - hw, x + hw, y + h, z + hw})
}

// Entity.push's numbers: the deadband under which a pair is one place, and
// each side's horizontal shove.
const (
	pushThreshold = 0.009999999776482582
	pushStrength  = 0.05000000074505806
)

// Pushable is isPushable: a live body pushes and is pushed unless on a
// wall. The server's boat pass also reads it.
func (m *Mob) Pushable() bool {
	return !m.Dead && !m.gone && !m.NoPush && !m.Climbing && m.Type != TypeBat && m.dragon == nil && !m.wardenUnderground()
}

// PushApart is Entity.push for one pair: the vector the body at the second
// position takes from the first. The farther axis carries the full share at
// a block or more, scaled by the root of the distance as they close, and
// nothing under the deadband.
func PushApart(fromX, fromZ, toX, toZ float64) (px, pz float64) {
	dx, dz := toX-fromX, toZ-fromZ
	farthest := math.Max(math.Abs(dx), math.Abs(dz))
	if farthest < pushThreshold {
		return 0, 0
	}
	farthest = math.Sqrt(farthest)
	dx /= farthest
	dz /= farthest
	share := 1 / farthest
	if share > 1 {
		share = 1
	}
	return dx * share * pushStrength, dz * share * pushStrength
}

// pushStep is LivingEntity.pushEntities, after the move as in aiStep: each
// pushable body overlapping this one is pushed apart and this one takes its
// share. Each pair is visited from both sides; against a player (not
// stepped by the server) this body takes both halves. Candidates come from
// the tick's grid (standing in for Level.getPushableEntities), else the
// plain list, in snapshot order either way.
func (m *Mob) pushStep(w World, players []Player, others []*Mob, events []MobEvent) []MobEvent {
	// Vehicles still make contact and count toward cramming, but take no
	// collision impulse. vanilla: Entity.push
	bw, h, pushable := m.width(), m.height(), m.Pushable() && !m.isVehicle()
	// vanilla: Entity.push returns while either body is noPhysics (a vex);
	// the cramming count still runs.
	ghost := m.Type == TypeVex
	// total counts the pushable bodies in the box, standing those of them
	// that ride nothing.
	total, standing := 0, 0
	for i := range players {
		p := &players[i]
		pw, ph, _ := p.box()
		if !p.Pushable || !boxesOverlap(m.X, m.Y, m.Z, bw, h,
			p.X, p.Y, p.Z, pw, ph) {
			continue
		}
		if !PushableBy(m.Team, m.TeamCollision, p.Team, p.TeamCollision) {
			continue
		}
		total++
		if !p.Passenger {
			standing++
		}
		if ghost {
			continue
		}
		if m.Type == TypeWarden {
			m.wardenTouch(&tickView{w: w, players: players, others: others}, *p, &events)
		}
		if m.Type == TypeSulfurCube {
			events = m.sulfurBurnPlayer(events, *p)
		}
		// vanilla: Entity.push, Entity.isPassengerOfSameVehicle
		if p.Passenger && m.RootVehicle().hasPlayerPassenger(p.EntityID) {
			continue
		}
		px, pz := PushApart(m.X, m.Z, p.X, p.Z)
		// The mob's own half, and the player's half besides.
		if pushable {
			m.VX -= px
			m.VZ -= pz
			m.VX -= px
			m.VZ -= pz
			m.Impulse = true
		}
	}
	pack := others
	if g := m.push; g != nil {
		pack = g.candidates(m, 0)
	}
	window := newSectionWindow(m.X-bw/2, m.X+bw/2, m.Y, m.Y+h, m.Z-bw/2, m.Z+bw/2)
	for _, other := range pack {
		if other == nil || other == m || !other.Pushable() {
			continue
		}
		if !window.holds(other) {
			continue
		}
		if !boxesOverlap(m.X, m.Y, m.Z, bw, h,
			other.X, other.Y, other.Z, other.width(), other.height()) {
			continue
		}
		if !PushableBy(m.Team, m.TeamCollision, other.Team, other.TeamCollision) {
			continue
		}
		total++
		if !other.RidesVehicle {
			standing++
		}
		if ghost {
			continue
		}
		if m.Type == TypeWarden {
			m.wardenTouch(&tickView{w: w, players: players, others: others}, other.asTarget(), &events)
		}
		if m.Type == TypeSulfurCube {
			events = m.sulfurBurnMob(events, other)
		}
		if other.Type == TypeIronGolem && !m.NoAI {
			events = m.cubeHitsGolem(w, other, events)
		}
		// A body and its own vehicle or fellow passengers are not pushed apart.
		// vanilla: Entity.push, Entity.isPassengerOfSameVehicle
		if m.RootVehicle() == other.RootVehicle() {
			continue
		}
		px, pz := PushApart(m.X, m.Z, other.X, other.Z)
		if pushable {
			m.VX -= px
			m.VZ -= pz
			m.Impulse = true
		}
		// The other body's own half is its own tick's to run.
		if !other.isVehicle() {
			other.VX += px
			other.VZ += pz
			other.Impulse = true
		}
	}
	// Boats and carts are pushed by their own passes; here they only count.
	if g := m.push; g != nil && g.crowd != nil {
		n := g.crowd.count(NewAABB(m.X-bw/2, m.Y, m.Z-bw/2, m.X+bw/2, m.Y+h, m.Z+bw/2), m.Team, m.TeamCollision)
		total += n
		standing += n
	}
	return m.crammed(w, total, standing, events)
}

// PushBody is a pushable boat or minecart as a mob's cramming counts it.
// vanilla: AbstractBoat.isPushable, AbstractMinecart.isPushable
type PushBody struct {
	Box  AABB
	Team string
	Rule CollisionRule
}

// PushCrowd is one tick's boats and minecarts in a world, filed in a grid.
type PushCrowd struct {
	Bodies []PushBody
	Grid   BoxGrid
}

// Add files a body.
func (c *PushCrowd) Add(b PushBody) {
	c.Grid.Add(int32(len(c.Bodies)), b.Box)
	c.Bodies = append(c.Bodies, b)
}

// Reset empties the crowd and keeps its storage.
func (c *PushCrowd) Reset() {
	clear(c.Bodies)
	c.Bodies = c.Bodies[:0]
	c.Grid.Reset()
}

// count is how many of the crowd's bodies cross box and are pushable by a
// body on team with rule. None rides anything.
// vanilla: Level.getPushableEntities, EntitySelector.pushableBy
func (c *PushCrowd) count(box AABB, team string, rule CollisionRule) int {
	n := 0
	c.Grid.Query(box, func(i int32) {
		if b := &c.Bodies[i]; box.Crosses(b.Box) && PushableBy(team, rule, b.Team, b.Rule) {
			n++
		}
	})
	return n
}

// crammed hurts a body 6 points a quarter of the ticks when more than
// max_entity_cramming-1 bodies share its box; the rule at 0 turns it off.
// Passengers are not counted.
// vanilla: LivingEntity.pushEntities
func (m *Mob) crammed(w World, total, standing int, events []MobEvent) []MobEvent {
	var limit int
	if g := m.push; g != nil {
		if !g.crammingSet {
			g.cramming, g.crammingSet = w.MaxEntityCramming(), true
		}
		limit = int(g.cramming)
	} else {
		limit = int(w.MaxEntityCramming())
	}
	if !rules.CrammingHurt(limit, total, standing, func() int { return m.Rand.Intn(4) }) {
		return events
	}
	if landed, soft := m.plainHurt(rules.CrammingDamage, rules.DamageTypeCramming, false); landed {
		m.noteLastDamage(rules.DamageTypeCramming)
		events = append(events, MobEvent{Mob: m, Kind: MobBurned,
			DamageType: rules.DamageTypeCramming, Softened: soft})
	}
	return events
}

// Push candidate gathering: a bucketed stand-in for the entity sections
// vanilla's AABB query walks, so pushing costs a neighbourhood, not the
// herd.
// vanilla: LivingEntity.pushEntities, Level.getPushableEntities,
// EntitySectionStorage.forEachAccessibleNonEmptySection

// pushCellSide is a grid cell's side in blocks, past the widest pair of
// ordinary half-widths, so a body's cell and its neighbours hold every
// overlap.
// vanilla: EntitySectionStorage.MAX_NON_CHONKY_ENTITY_SIZE
const pushCellSide = 4.0

// pushCellKey is a grid cell by x and z packed into one word, which the map
// hashes on its fast path. Height is left to boxesOverlap.
type pushCellKey int64

// cellKey packs cell coordinates. Cells 2^32 apart would share a key, far
// past the world border; boxesOverlap filters whatever a shared key brings.
func cellKey(x, z int) pushCellKey {
	return pushCellKey(int64(x)<<32 | int64(uint32(z)))
}

// keyCoords is cellKey's inverse.
func keyCoords(k pushCellKey) (x, z int) { return int(int32(int64(k) >> 32)), int(int32(k)) }

// cellCoords is the cell's x and z.
func cellCoords(x, z float64) (cx, cz int) {
	return int(math.Floor(x / pushCellSide)), int(math.Floor(z / pushCellSide))
}

// cellOf is the cell the point (x, z) stands in.
func cellOf(x, z float64) pushCellKey {
	return cellKey(cellCoords(x, z))
}

// pushEntry is a grid slot holding the mob and its box's bounds, so the
// filter reads cell memory instead of chasing pointers and does no sums. moved
// and refresh keep the bounds current.
type pushEntry struct {
	m          *Mob
	minX, maxX float64
	minY, maxY float64
	minZ, maxZ float64
}

func (g *pushGrid) entryOf(m *Mob) pushEntry {
	hw := m.width() / 2
	if hw > g.wide {
		g.wide = hw
	}
	return pushEntry{
		m:    m,
		minX: m.X - hw, maxX: m.X + hw,
		minY: m.Y, maxY: m.Y + m.height(),
		minZ: m.Z - hw, maxZ: m.Z + hw,
	}
}

// pushGrid buckets one tick's herd into cells.
type pushGrid struct {
	// cramming is the max_entity_cramming rule, read once per tick on first
	// use (crammingSet); the rule cannot change inside a tick.
	cramming    int32
	crammingSet bool
	// crowd is the tick's boats and minecarts, nil for none.
	crowd *PushCrowd
	cells map[pushCellKey][]pushEntry
	// dense covers compact herds; cells then holds only moves outside its bounds.
	dense   []pushBucket
	entries []pushEntry
	rect    denseRect
	// present marks the types in the snapshot, set by build.
	present [registry.EntityTypeCount]bool
	// byID is the snapshot by entity ID, the first mob filed winning a
	// shared ID; kept between ticks like cells.
	byID map[int32]*Mob
	// wide bounds the half-width of every box filed this tick.
	wide float64
	// caravan is the snapshot's llamas that are leashed or in a caravan, plus
	// those that joined one this tick: the only ones a caravan goal can pick.
	caravan []*Mob
	// keys is the sparse query's scratch of occupied cells.
	keys []pushCellKey
	// near is the candidates' scratch, reused gathering to gathering.
	near []*Mob
	// gathered, when set, adds up the candidates handed out.
	gathered *int
	// spare holds emptied cell slices a new cell can take.
	spare [][]pushEntry
}

// pushBucket starts as a counting-sort segment. spare keeps any growth caused
// by moves between cells, so subsequent ticks can reuse that allocation.
type pushBucket struct {
	entries []pushEntry
	spare   []pushEntry
	count   int
}

// build files the snapshot in order, using at most four cell heads per mob.
func (g *pushGrid) build(mobs []*Mob) {
	g.present = [registry.EntityTypeCount]bool{}
	for _, m := range mobs {
		g.present[m.Type] = true
	}
	if len(mobs) == 0 {
		return
	}
	minX, minZ := cellCoords(mobs[0].X, mobs[0].Z)
	maxX, maxZ := minX, minZ
	for _, m := range mobs[1:] {
		x, z := cellCoords(m.X, m.Z)
		minX, minZ = min(minX, x), min(minZ, z)
		maxX, maxZ = max(maxX, x), max(maxZ, z)
	}
	if total, ok := g.rect.set(minX, maxX, minZ, maxZ, 4*len(mobs)); ok {
		g.dense = slices.Grow(g.dense, total)[:total]
		for _, m := range mobs {
			x, z := cellCoords(m.X, m.Z)
			g.dense[g.rect.row(x)+z-g.rect.minZ].count++
		}
		g.entries = slices.Grow(g.entries, len(mobs))[:len(mobs)]
		offset := 0
		for i := range g.dense {
			c := &g.dense[i]
			end := offset + c.count
			c.entries = g.entries[offset:offset:end]
			if cap(c.spare) >= c.count && cap(c.spare) > 0 {
				c.entries = c.spare[:0]
			}
			offset = end
		}
	}
	for i, m := range mobs {
		g.add(m, i)
	}
}

// denseIndex returns -1 outside the tick's dense rectangle.
func (g *pushGrid) denseIndex(cell pushCellKey) int {
	return g.rect.index(int(int32(int64(cell)>>32)), int(int32(cell)))
}

func (g *pushGrid) cell(cell pushCellKey) []pushEntry {
	if i := g.denseIndex(cell); i >= 0 {
		return g.dense[i].entries
	}
	return g.cells[cell]
}

func (g *pushGrid) setCell(cell pushCellKey, entries []pushEntry) {
	if i := g.denseIndex(cell); i >= 0 {
		g.dense[i].entries = entries
	} else {
		g.cells[cell] = entries
	}
}

var pushGrids sync.Pool

// takePushGrid is an empty grid sized for n mobs, its map and cell slices
// kept from an earlier tick.
func takePushGrid(n int, gathered *int) *pushGrid {
	g, _ := pushGrids.Get().(*pushGrid)
	if g == nil {
		g = &pushGrid{cells: make(map[pushCellKey][]pushEntry, n), byID: make(map[int32]*Mob, n)}
	}
	g.gathered = gathered
	return g
}

// release empties the grid, dropping its mob pointers, and keeps the cell
// slices for the next tick's cells.
func (g *pushGrid) release() {
	g.reset()
	pushGrids.Put(g)
}

// reset drops mob pointers while keeping the tick's storage.
func (g *pushGrid) reset() {
	for _, c := range g.cells {
		clear(c)
		g.spare = append(g.spare, c[:0])
	}
	clear(g.cells)
	for i := range g.dense {
		c := &g.dense[i]
		clear(c.entries)
		clear(c.spare[:cap(c.spare)])
		c.entries, c.count = nil, 0
	}
	g.dense = g.dense[:0]
	clear(g.entries)
	g.entries = g.entries[:0]
	g.rect.reset()
	clear(g.byID)
	clear(g.near[:cap(g.near)])
	g.near = g.near[:0]
	clear(g.caravan)
	g.caravan = g.caravan[:0]
	g.wide, g.gathered, g.crammingSet, g.crowd = 0, nil, false, nil
}

// file appends e to cell and returns its slot.
func (g *pushGrid) file(cell pushCellKey, e pushEntry) int {
	if i := g.denseIndex(cell); i >= 0 {
		c := &g.dense[i]
		slot := len(c.entries)
		if slot == cap(c.entries) {
			c.spare = append(c.spare[:0], c.entries...)
			c.spare = append(c.spare, e)
			c.entries = c.spare
		} else {
			c.entries = append(c.entries, e)
		}
		return slot
	}
	c, ok := g.cells[cell]
	if !ok && len(g.spare) > 0 {
		c = g.spare[len(g.spare)-1]
		g.spare = g.spare[:len(g.spare)-1]
	}
	g.cells[cell] = append(c, e)
	return len(c)
}

// add buckets a snapshot mob with its index.
func (g *pushGrid) add(m *Mob, idx int) {
	m.pushIdx = idx
	m.pushCell = cellOf(m.X, m.Z)
	m.pushSlot = g.file(m.pushCell, g.entryOf(m))
	if (m.inCaravan() || m.leashed()) && IsLlama(m.Type) {
		g.caravan = append(g.caravan, m)
	}
	if _, ok := g.byID[m.ID]; !ok {
		g.byID[m.ID] = m
	}
}

// moved re-files a mob that crossed a cell boundary this tick (a knockback
// may cross many) and refreshes its box.
func (g *pushGrid) moved(m *Mob) {
	cell := cellOf(m.X, m.Z)
	if cell == m.pushCell {
		g.refresh(m)
		return
	}
	if c := g.cell(m.pushCell); m.pushSlot < len(c) && c[m.pushSlot].m == m {
		last := len(c) - 1
		c[m.pushSlot] = c[last]
		c[m.pushSlot].m.pushSlot = m.pushSlot
		g.setCell(m.pushCell, c[:last])
	}
	m.pushCell = cell
	m.pushSlot = g.file(cell, g.entryOf(m))
}

// refresh rewrites a mob's box copy; a mob not in the grid (added after the
// snapshot) is ignored.
func (g *pushGrid) refresh(m *Mob) {
	if c := g.cell(m.pushCell); m.pushSlot < len(c) && c[m.pushSlot].m == m {
		c[m.pushSlot] = g.entryOf(m)
	}
}

// candidates is gather sorted into snapshot order so pushes land as the
// whole-herd scan would. Positions do not change during a push step, so
// pushStep's own overlap test passes exactly these; sorting after the cut
// keeps the sort small. The result is scratch valid until the next query.
func (g *pushGrid) candidates(m *Mob, inflate float64) []*Mob {
	g.gather(m, inflate)
	slices.SortFunc(g.near, func(a, b *Mob) int { return cmp.Compare(a.pushIdx, b.pushIdx) })
	return g.near
}

// gather collects the mobs in the cells m's inflated box can reach that
// overlap the box, in no set order (pushIdx is each one's snapshot order).
// The result is scratch valid until the next query.
func (g *pushGrid) gather(m *Mob, inflate float64) []*Mob {
	g.near = g.near[:0]
	hw := m.width() / 2
	// The same sums boxesOverlap does, once for the query.
	qw, qh := hw+inflate, m.height()+inflate
	qMinX, qMaxX := m.X-qw, m.X+qw
	qMinY, qMaxY := m.Y-inflate, m.Y+qh
	qMinZ, qMaxZ := m.Z-qw, m.Z+qw
	// A target can overlap even when its centre is outside the query box.
	minX, minZ := cellCoords(qMinX-g.wide, qMinZ-g.wide)
	maxX, maxZ := cellCoords(qMaxX+g.wide, qMaxZ+g.wide)
	seen := 0
	collect := func(c []pushEntry) {
		seen += len(c)
		for i := range c {
			o := &c[i]
			if qMaxX > o.minX && qMinX < o.maxX && qMaxY > o.minY && qMinY < o.maxY && qMaxZ > o.minZ && qMinZ < o.maxZ {
				g.near = append(g.near, o.m)
			}
		}
	}
	if g.rect.active() && len(g.cells) == 0 {
		loX, hiX, loZ, hiZ := g.rect.clip(minX, maxX, minZ, maxZ)
		for x := loX; x <= hiX; x++ {
			row := g.rect.row(x)
			for z := loZ; z <= hiZ; z++ {
				collect(g.dense[row+z-g.rect.minZ].entries)
			}
		}
	} else if (maxX-minX+1)*(maxZ-minZ+1) > len(g.cells)+len(g.dense) {
		// Visit occupied cells in cell order when the query spans more cells.
		g.keys = g.keys[:0]
		for k := range g.cells {
			if x, z := keyCoords(k); x >= minX && x <= maxX && z >= minZ && z <= maxZ {
				g.keys = append(g.keys, k)
			}
		}
		loX, hiX, loZ, hiZ := g.rect.clip(minX, maxX, minZ, maxZ)
		for x := loX; x <= hiX; x++ {
			row := g.rect.row(x)
			for z := loZ; z <= hiZ; z++ {
				if len(g.dense[row+z-g.rect.minZ].entries) > 0 {
					g.keys = append(g.keys, cellKey(x, z))
				}
			}
		}
		slices.SortFunc(g.keys, func(a, b pushCellKey) int {
			ax, az := keyCoords(a)
			bx, bz := keyCoords(b)
			return cmp.Or(cmp.Compare(ax, bx), cmp.Compare(az, bz))
		})
		for _, k := range g.keys {
			collect(g.cell(k))
		}
	} else {
		// Mixed dense and overflow cells still traverse in cell order before
		// sorting, including ties from bodies outside the snapshot.
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				collect(g.cell(cellKey(x, z)))
			}
		}
	}
	if g.gathered != nil {
		*g.gathered += seen
	}
	return g.near
}

// boxesOverlap is the AABB intersection the pushable list is gathered by.
func boxesOverlap(ax, ay, az, aw, ah, bx, by, bz, bw, bh float64) bool {
	hwA, hwB := aw/2, bw/2
	return ax+hwA > bx-hwB && ax-hwA < bx+hwB &&
		ay+ah > by && ay < by+bh &&
		az+hwA > bz-hwB && az-hwA < bz+hwB
}

// bankFall adds the drop from (oldX, oldY, oldZ) to the fall, clearing it in
// water; rises never subtract. A step of a block or more through a web or
// water clears it on the way (FallResetSweep), so a fast fall through a
// one-thick layer is forgiven.
func (m *Mob) bankFall(w World, oldX, oldY, oldZ float64) {
	feet := m.cell()
	if rules.FallResetSweep(w, m.FallDistance, player.Position{X: oldX, Y: oldY, Z: oldZ},
		player.Position{X: m.X, Y: m.Y, Z: m.Z}, false, false) {
		m.FallDistance = 0
	}
	if rules.IsWater(w.BlockAt(feet.X, feet.Y, feet.Z)) {
		m.FallDistance = 0
	} else if dy := m.Y - oldY; dy < -physicsEpsilon {
		m.FallDistance += -dy
	}
}

// fallParams are the safe distance and damage multiplier, including modifiers.
// vanilla: LivingEntity.calculateFallPower, calculateFallDamage
func (m *Mob) fallParams() (safe, multiplier float64) {
	if m.attrs != nil {
		rules.UpdateEffectAttributesOf(m.attrs, m.effects.All(), registry.AttributeSafeFallDistance)
		return m.attrOr(registry.AttributeSafeFallDistance, 3), m.attrOr(registry.AttributeFallDamageMultiplier, 1)
	}
	safe, multiplier = m.fallAttributeBases()
	// Unedited mobs have no map; only their effects can change these bases.
	return safe + rules.SafeFallDistance(m.effects.All()) - 3, multiplier
}

// fallBlow hurts the body for a landing, reporting the damage, whether it
// landed whole or softened, and the block's damage type.
// vanilla: LivingEntity.causeFallDamage, Block.fallOn
func (m *Mob) fallBlow(w World, landed float64) (damage float64, hurt, soft bool, effect rules.FallEffect) {
	if m.fallDamageImmune() {
		return 0, false, false, rules.FallEffect{Scale: 1, Modifier: 1, DamageType: rules.DamageTypeFall}
	}
	block := rules.FallOn(rules.OnPos(w, m.X, m.Y, m.Z, m.BbWidth()/2))
	d, hurt, soft := m.fallHurt(landed, block)
	return d, hurt, soft, block
}

// fallHurt applies a landing of the given distance under a block's effect.
func (m *Mob) fallHurt(landed float64, block rules.FallEffect) (damage float64, hurt, soft bool) {
	safe, multiplier := m.fallParams()
	d := 0.0
	if landed > 0 {
		d = rules.FallDamage(landed*block.Scale+block.Offset, safe, multiplier*block.Modifier)
	}
	if m.Type == TypeFrog {
		d -= 5
	} // vanilla: Frog.calculateFallDamage
	if m.Type == TypeGoat && d > 0 {
		// vanilla: Goat.calculateFallDamage
		d -= goatFallReduction
	}
	if d <= 0 {
		return 0, false, false
	}
	hurt, soft = m.plainHurt(d, block.DamageType, false)
	if hurt {
		m.noteFallHurt(block.DamageType, landed)
	}
	return d, hurt, soft
}

// LandOnCart hurts a mob riding a cart by the fall the cart landed, and
// reports the damage, whether it was hurt and whether the hurt was softened.
// vanilla: Entity.propagateFallToPassengers
func (m *Mob) LandOnCart(landed float64, block rules.FallEffect) (damage float64, hurt, soft bool) {
	if m.fallDamageImmune() {
		return 0, false, false
	}
	return m.fallHurt(landed, block)
}

// CollisionRule is a team's collision rule, numbered as scoreboard.CollisionRule.
// vanilla: Team.CollisionRule
type CollisionRule int32

const (
	CollideAlways CollisionRule = iota
	CollideNever
	PushOtherTeams
	PushOwnTeam
)

// PushableBy says whether a body on theirTeam is pushed by (and counted for the
// cramming of) a body on ownTeam; "" is no team.
// vanilla: EntitySelector.pushableBy
func PushableBy(ownTeam string, ownRule CollisionRule, theirTeam string, theirRule CollisionRule) bool {
	if ownRule == CollideNever || theirRule == CollideNever {
		return false
	}
	sameTeam := ownTeam != "" && ownTeam == theirTeam
	if (ownRule == PushOwnTeam || theirRule == PushOwnTeam) && sameTeam {
		return false
	}
	return (ownRule != PushOtherTeams && theirRule != PushOtherTeams) || sameTeam
}

// computeModifiedFriction scales a friction toward 1 by the attribute.
// vanilla: LivingEntity.computeModifiedFriction
func computeModifiedFriction(friction, modifier float32) float32 {
	return max(0, min(1, 1-(1-friction)*modifier))
}

// airDrags are travelInAir's horizontal and vertical air drag; a filled
// sulfur cube uses the horizontal drag on every axis.
// vanilla: LivingEntity.getAirDrag, omnidirectionalAirMover
func (m *Mob) airDrags() (horizontal, vertical float64) {
	modifier := 1.0
	if m.Type == TypeSulfurCube {
		modifier = m.sulfurProf().drag
	}
	mod := float32(m.attrOr(registry.AttributeAirDragModifier, modifier))
	horizontal = float64(computeModifiedFriction(0.91, mod))
	if m.SulfurCubeFilled() {
		return horizontal, horizontal
	}
	return horizontal, float64(computeModifiedFriction(0.98, mod))
}

// bounciness is the body's collision restitution.
// vanilla: LivingEntity.getEntityBounciness
func (m *Mob) bounciness() float64 {
	base := 0.0
	if m.Type == TypeSulfurCube {
		base = m.sulfurProf().bounciness
	}
	return m.attrOr(registry.AttributeBounciness, base)
}

// bounceOnLanding is the vertical velocity after landing: the larger of the
// body's and the block's restitution unless the block suppresses bounces.
// vanilla: Entity.restituteMovementAfterCollisions
func (m *Mob) bounceOnLanding(w World, moved float64) float64 {
	gravity := m.effectiveGravity()
	if -m.VY <= gravity {
		return 0
	}
	state := rules.OnPos(w, m.X, m.Y, m.Z, m.width()/2)
	rest := 0.0
	if !registry.BlockOf(state).InTag(registry.TagIDSuppressesBounce) {
		rest = math.Max(m.bounciness(), rules.BlockBounceRestitution(state, true))
	}
	_, drag := m.airDrags()
	return rules.BouncedFall(m.VY, moved, gravity, drag, rest)
}

// bounceOffCeiling is the velocity after the head hits a block: only the
// body's own bounciness gives anything back.
func (m *Mob) bounceOffCeiling(moved float64) float64 {
	rest := m.bounciness()
	if rest <= 0 || m.VY == 0 {
		return 0
	}
	_, drag := m.airDrags()
	return rules.RestitutedMovement(m.VY, moved, m.effectiveGravity(), drag, rest)
}

// bounceOffWall is the horizontal velocity after a collision on that axis.
func (m *Mob) bounceOffWall(v float64) float64 {
	return -v * m.bounciness()
}
