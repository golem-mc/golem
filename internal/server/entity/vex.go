package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The vex: a small fire-immune flier that passes through blocks, charges
// with a sword and dies when its summoned life runs out. Not modelled: the
// summoner's team.
// vanilla: Vex, Vex.VexMoveControl, Vex.VexChargeAttackGoal

// TypeVex is the vex.
const TypeVex = registry.EntityTypeVex

// The vex's numbers.
// vanilla: Vex, Vex.VexChargeAttackGoal, Vex.VexRandomMoveGoal
const (
	// vexLifeBeat is the ticks between two starvation hurts once the life
	// ran out.
	vexLifeBeat = 20
	// vexBoxSize is AABB.getSize of the 0.4 by 0.8 box: the mean edge.
	vexBoxSize = (0.4 + 0.8 + 0.4) / 3
	// vexNear is the squared distance below which a charge reaims every tick.
	vexNear = 9.0
	// vexChargeMin is the squared distance a charge starts beyond.
	vexChargeMin = 4.0
	// vexChargeRoll and vexWanderRoll are the nextInt bounds of the goals'
	// start rolls, before reducedTickDelay.
	vexChargeRoll = 7
	vexWanderRoll = 7
	// vexWanderPace is the speed modifier of a wander leg.
	vexWanderPace = 0.25
	// vexPull is the share of the way to the wanted point added to the
	// velocity each tick, times the speed modifier.
	vexPull = 0.05
	// vexStarveDamage is the hurt an aged-out vex takes every beat.
	vexStarveDamage = 1.0
	// vexSwordChance is the main hand's drop chance, set to nothing.
	vexSwordChance = 0
)

func init() {
	// vanilla: EntityTypes.VEX and Vex.createAttributes
	kinds[TypeVex] = &kind{
		name: "Vex", width: 0.4, height: 0.8, eyeHeight: 0.51875,
		maxHealth: 14, speed: float64(float32(0.7)), hostile: true, fireImmune: true,
		attackDamage: 4,
		sounds: MobSounds{
			Ambient: registry.SoundEntityVexAmbient,
			Hurt:    registry.SoundEntityVexHurt,
			Death:   registry.SoundEntityVexDeath,
		},
	}
	mobTracking[TypeVex] = tracking{rangeChunks: 8, updateInterval: 3}
}

// vexState is what a vex keeps beyond the common mob.
type vexState struct {
	// charging is DATA_FLAGS_ID bit 0x01.
	charging bool
	// limited and lifeTicks are hasLimitedLife and limitedLifeTicks.
	limited   bool
	lifeTicks int
	// bound is the block the vex wanders around, boundSet false for none.
	bound    Waypoint
	boundSet bool
	// ownerID is the entity ID of the summoner, 0 for none; ownerUUID is the
	// same body's identity, which survives the renumbering a reload gives the
	// evoker.
	// vanilla: Vex.owner, an EntityReference the file stores as a UUID
	ownerID   int32
	ownerUUID uuid.UUID
	// wanted is the move control's MOVE_TO operation toward (wx, wy, wz) at
	// the speed modifier pace.
	wanted     bool
	wx, wy, wz float64
	pace       float64
}

// VexCharging is the vex's charging flag.
func (m *Mob) VexCharging() bool { return m.vex.charging }

// VexOwnerID is the entity ID of the evoker that summoned the vex, 0 for none.
func (m *Mob) VexOwnerID() int32 { return m.vex.ownerID }

// VexOwnerUUID is the identity of the evoker that summoned the vex, the nil
// UUID for none; unlike VexOwnerID it survives a reload.
func (m *Mob) VexOwnerUUID() uuid.UUID { return m.vex.ownerUUID }

// VexLife returns the ticks left of a limited life.
func (m *Mob) VexLife() (ticks int, limited bool) { return m.vex.lifeTicks, m.vex.limited }

// VexBound returns the block the vex wanders around.
func (m *Mob) VexBound() (x, y, z int32, ok bool) {
	return m.vex.bound.X, m.vex.bound.Y, m.vex.bound.Z, m.vex.boundSet
}

// SetVexLife gives the vex a limited life of the ticks given.
// vanilla: Vex.setLimitedLife
func (m *Mob) SetVexLife(ticks int) {
	m.vex.limited = true
	m.vex.lifeTicks = ticks
}

// SetVexBound sets the block the vex wanders around.
// vanilla: Vex.setBoundOrigin
func (m *Mob) SetVexBound(x, y, z int32) {
	m.vex.bound = Waypoint{x, y, z}
	m.vex.boundSet = true
}

// SetVexOwner sets the summoning evoker.
// vanilla: Vex.setOwner
func (m *Mob) SetVexOwner(id int32) { m.vex.ownerID = id }

// SetVexOwnerUUID sets the summoner's saved identity, which a load resolves
// back to the evoker's rewritten entity ID.
// vanilla: Vex.setOwner, whose EntityReference resolves by identity
func (m *Mob) SetVexOwnerUUID(id uuid.UUID) { m.vex.ownerUUID = id }

// finalizeVex is Vex.populateDefaultEquipmentSlots: an iron sword that never
// drops, then the weapon enchantment roll.
func (m *Mob) finalizeVex(local rules.LocalDifficulty) {
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemIronSword), Count: 1})
	m.DropChances[0] = vexSwordChance
	m.rollSpawnEnchantments(m.equipmentDice(), local)
}

// vexGoals is Vex.registerGoals: float 0, charge 4, wander 8, looks 9 (3
// blocks, every tick) and 10; grudge 1, owner's target 2, nearest player 3.
func vexGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: isRaiderType},
		&vexCopyOwnerTargetGoal{m: m},
		&nearestTargetGoal{m: m, priority: 3},
		&vexChargeGoal{m: m},
		&vexWanderGoal{m: m},
		&lookAtPlayer{m: m, priority: 9, reach: 3, chance: 1},
		&lookAtMob{m: m, priority: 10},
	}
}

// vexWant is MoveControl.setWantedPosition.
func (m *Mob) vexWant(x, y, z, pace float64) {
	v := &m.vex
	v.wanted, v.wx, v.wy, v.wz, v.pace = true, x, y, z, pace
}

// vexMoveStep is the vex's tick after its goals: the move control, the flight
// through blocks and the life's countdown.
// vanilla: Vex.tick, Vex.VexMoveControl.tick
func (m *Mob) vexMoveStep(t *tickView) {
	v := &m.vex
	if v.wanted {
		dx, dy, dz := v.wx-m.X, v.wy-m.Y, v.wz-m.Z
		length := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if length < vexBoxSize {
			v.wanted = false
			m.VX, m.VY, m.VZ = m.VX*0.5, m.VY*0.5, m.VZ*0.5
		} else {
			pull := v.pace * vexPull / length
			m.VX += dx * pull
			m.VY += dy * pull
			m.VZ += dz * pull
			if p := m.targetIn(t); p != nil {
				m.Yaw = float32(yawToward(p.X-m.X, p.Z-m.Z))
			} else {
				m.Yaw = float32(yawToward(m.VX, m.VZ))
			}
		}
	}
	// noPhysics skips collision only: no gravity, but travel's fluid drag
	// still applies.
	// vanilla: LivingEntity.travelInWater, LivingEntity.travelInLava
	m.jumpStep(t.w)
	m.jump = false
	m.X += m.VX
	m.Y += m.VY
	m.Z += m.VZ
	m.OnGround = false
	switch {
	case m.InWater():
		m.VX *= flyWaterDrag
		m.VY *= flyWaterDrag
		m.VZ *= flyWaterDrag
	case m.InLava():
		m.VX *= flyLavaDrag
		m.VY *= flyLavaDrag
		m.VZ *= flyLavaDrag
	default:
		m.VX *= airDrag
		m.VY *= verticalDrag
		m.VZ *= airDrag
	}

	if v.limited {
		v.lifeTicks--
		if v.lifeTicks <= 0 {
			v.lifeTicks = vexLifeBeat
			if landed, soft := m.plainHurt(vexStarveDamage, rules.DamageTypeStarve, false); landed {
				m.noteLastDamage(rules.DamageTypeStarve)
				if !soft {
					t.events = append(t.events, MobEvent{Mob: m, Kind: MobBurned, DamageType: rules.DamageTypeStarve})
				}
			}
		}
	}
}

// vexChargeGoal is Vex.VexChargeAttackGoal.
type vexChargeGoal struct{ m *Mob }

func (g *vexChargeGoal) Priority() int    { return 4 }
func (g *vexChargeGoal) Flags() GoalFlags { return GoalMove }

func (g *vexChargeGoal) CanStart(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil || g.m.vex.wanted || g.m.Rand.Intn(reducedTickDelay(vexChargeRoll)) != 0 {
		return false
	}
	return Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) > vexChargeMin
}

func (g *vexChargeGoal) ShouldContinue(t *tickView) bool {
	return g.m.vex.wanted && g.m.vex.charging && g.m.targetIn(t) != nil
}

func (g *vexChargeGoal) Start(t *tickView) {
	m := g.m
	if p := m.targetIn(t); p != nil {
		m.vexWant(p.X, p.Y+p.eyeHeight(), p.Z, 1.0)
	}
	m.SetMetadataVexCharging(true)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityVexCharge, Volume: 1, Pitch: 1})
}

func (g *vexChargeGoal) Stop(*tickView) { g.m.SetMetadataVexCharging(false) }

func (g *vexChargeGoal) RequiresEveryTick() bool { return true }

func (g *vexChargeGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	if vexTouches(m, p) {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: m.attackDamage(), Player: *p})
		m.SetMetadataVexCharging(false)
		return
	}
	if Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z) < vexNear {
		m.vexWant(p.X, p.Y+p.eyeHeight(), p.Z, 1.0)
	}
}

// vexTouches is the vex's box meeting the target's, the target taken at a
// player's 0.6 by 1.8.
func vexTouches(m *Mob, p *Player) bool {
	const vexHalf = 0.2
	pw, ph, _ := p.box()
	return m.X-vexHalf < p.X+pw/2 && m.X+vexHalf > p.X-pw/2 &&
		m.Y < p.Y+ph && m.Y+0.8 > p.Y &&
		m.Z-vexHalf < p.Z+pw/2 && m.Z+vexHalf > p.Z-pw/2
}

// vexWanderGoal is Vex.VexRandomMoveGoal: three tries at an empty block near
// the bound origin.
type vexWanderGoal struct{ m *Mob }

func (g *vexWanderGoal) Priority() int    { return 8 }
func (g *vexWanderGoal) Flags() GoalFlags { return GoalMove }

func (g *vexWanderGoal) CanStart(*tickView) bool {
	return !g.m.vex.wanted && g.m.Rand.Intn(reducedTickDelay(vexWanderRoll)) == 0
}

func (g *vexWanderGoal) ShouldContinue(*tickView) bool { return false }

func (g *vexWanderGoal) Start(*tickView) {}

func (g *vexWanderGoal) Stop(*tickView) {}

func (g *vexWanderGoal) RequiresEveryTick() bool { return false }

func (g *vexWanderGoal) Tick(t *tickView) {
	m := g.m
	origin := m.cell()
	if m.vex.boundSet {
		origin = m.vex.bound
	}
	for range 3 {
		x := origin.X + int32(m.Rand.Intn(15)) - 7
		y := origin.Y + int32(m.Rand.Intn(11)) - 5
		z := origin.Z + int32(m.Rand.Intn(15)) - 7
		if !world.IsAirState(t.w.BlockAt(x, y, z)) {
			continue
		}
		m.vexWant(float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, vexWanderPace)
		break
	}
}

// vexCopyOwnerTargetGoal is Vex.VexCopyOwnerTargetGoal: take on whatever the
// summoner hunts.
type vexCopyOwnerTargetGoal struct{ m *Mob }

func (g *vexCopyOwnerTargetGoal) Priority() int    { return 2 }
func (g *vexCopyOwnerTargetGoal) Flags() GoalFlags { return GoalTarget }

// ownerTarget is the entity ID the summoner hunts, 0 for none.
func (g *vexCopyOwnerTargetGoal) ownerTarget(t *tickView) int32 {
	if g.m.vex.ownerID == 0 {
		return 0
	}
	owner := t.mobByID(g.m.vex.ownerID)
	if owner == nil || !owner.Alive() {
		return 0
	}
	return owner.TargetID
}

func (g *vexCopyOwnerTargetGoal) CanStart(t *tickView) bool {
	id := g.ownerTarget(t)
	if id == 0 {
		return false
	}
	saved := g.m.TargetID
	g.m.SetMetadataTargetID(id)
	ok := g.m.targetIn(t) != nil
	g.m.SetMetadataTargetID(saved)
	return ok
}

func (g *vexCopyOwnerTargetGoal) Start(t *tickView) { g.m.SetMetadataTargetID(g.ownerTarget(t)) }

func (g *vexCopyOwnerTargetGoal) ShouldContinue(t *tickView) bool {
	return g.m.targetIn(t) != nil && g.m.inFollowRange(g.m.targetIn(t))
}

func (g *vexCopyOwnerTargetGoal) Tick(*tickView) {}

func (g *vexCopyOwnerTargetGoal) Stop(*tickView) { g.m.SetMetadataTargetID(0) }

func (g *vexCopyOwnerTargetGoal) RequiresEveryTick() bool { return false }
