package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The ravager: a raider that charges its target, is stunned or shoved by a
// blocked blow, roars to hurt and throw what stands around it, and tramples
// through leaves. Not modelled: the jockeys it carries in later waves,
// its attack box and the stun particles (drawn by the client).
// vanilla: Ravager

// TypeRavager is the ravager.
const TypeRavager = registry.EntityTypeRavager

// The ravager's numbers.
// vanilla: Ravager
const (
	ravagerAttackTicks = 10 // ATTACK_DURATION
	ravagerStunTicks   = 40 // STUN_DURATION
	// ravagerRoarTicks is the roar countdown a stun's end starts; the roar
	// lands when it reaches ravagerRoarAt.
	ravagerRoarTicks  = 20
	ravagerRoarAt     = 10
	ravagerRoarReach  = 4.0
	ravagerRoarDamage = 6.0
	ravagerWalkPace   = 0.3 // BASE_MOVEMENT_SPEED
	ravagerHuntPace   = 0.35
	// ravagerPaceBlend is the share of the way to the wanted pace the base
	// speed covers each tick.
	ravagerPaceBlend = 0.1
	ravagerStunOdds  = 0.5
	// ravagerThrow is strongKnockback's horizontal push, ravagerLift its
	// vertical one.
	ravagerThrow = 4.0
	ravagerLift  = 0.2
	// ravagerLeafReach is the box inflate that finds the leaves in the way.
	ravagerLeafReach = 0.2
	// ravagerShove is Entity.push(Entity)'s scale of the shove a stunning
	// block gives the defender.
	ravagerShove = 0.05
)

// leaves is the minecraft:leaves block tag, which LeavesBlock's subclasses
// make up.
var leaves = registry.BlockTagOf("minecraft:leaves")

// IsLeaves reports whether the block state is a leaf block.
func IsLeaves(state uint16) bool { return registry.BlockOf(state).InTag(leaves) }

func init() {
	// vanilla: EntityTypes.RAVAGER and Ravager.createAttributes
	kinds[TypeRavager] = &kind{
		maxHeadYRot: 45,
		name:        "Ravager", width: 1.95, height: 2.2,
		maxHealth: 100, speed: ravagerWalkPace, hostile: true, followRange: 32,
		attackDamage: 12, attackKnockback: 1.5, knockbackResist: 0.75, stepHeight: 1,
		sounds: MobSounds{
			Ambient: registry.SoundEntityRavagerAmbient,
			Hurt:    registry.SoundEntityRavagerHurt,
			Death:   registry.SoundEntityRavagerDeath,
			Step:    registry.SoundEntityRavagerStep,
		},
	}
	mobTracking[TypeRavager] = tracking{rangeChunks: 10, updateInterval: 3}
}

// ravagerState is what a ravager keeps beyond the common mob.
type ravagerState struct {
	// attack, stun and roar are attackTick, stunnedTick and roarTick.
	attack, stun, roar int
	// pace is the movement_speed base, which eases toward the wanted pace.
	pace float64
}

// RavagerTicks is attackTick, stunnedTick and roarTick, what a saved ravager
// keeps.
func (m *Mob) RavagerTicks() (attack, stun, roar int) {
	return m.ravager.attack, m.ravager.stun, m.ravager.roar
}

// SetRavagerTicks restores attackTick, stunnedTick and roarTick.
func (m *Mob) SetRavagerTicks(attack, stun, roar int) {
	m.ravager.attack, m.ravager.stun, m.ravager.roar = attack, stun, roar
}

// ravagerImmobile is Ravager.isImmobile: mid-swing, stunned or roaring.
func (m *Mob) ravagerImmobile() bool {
	r := &m.ravager
	return m.Type == TypeRavager && (r.attack > 0 || r.stun > 0 || r.roar > 0)
}

// ravagerGoals wires Ravager.registerGoals in priority order across both
// selectors: the illagers' targets, the charge at 4, stroll 5 at 0.4 and the
// looks at 6 and 10.
func ravagerGoals(m *Mob) []Goal {
	m.ravager.pace = ravagerWalkPace
	goals := append([]Goal{newFloatGoal(m)}, illagerTargets(m, false)...)
	return withRaidGoals(m, append(goals,
		&meleeAttackGoal{m: m, priority: 4, followUnseen: true, pace: 1,
			swingStatus: protocol.EntityStatusRavagerAttack, swingSound: registry.SoundEntityRavagerAttack,
			onSwing: func() { m.ravager.attack = ravagerAttackTicks }},
		&randomStroll{m: m, priority: 5, pace: 0.4},
		&lookAtPlayer{m: m, priority: 6, reach: 6},
		&lookAtMob{m: m, priority: 10},
	))
}

// ravagerStep is Ravager.aiStep's part behind the goals: the pace, the leaves
// in the way and the three countdowns.
// vanilla: Ravager.aiStep
func (m *Mob) ravagerStep(t *tickView) {
	r := &m.ravager
	if !m.Alive() {
		return
	}
	if m.ravagerImmobile() {
		r.pace = 0
	} else {
		want := ravagerWalkPace
		if m.TargetID != 0 {
			want = ravagerHuntPace
		}
		r.pace += (want - r.pace) * ravagerPaceBlend
	}
	if m.horizontalCollision && t.w.MobGriefing() {
		m.ravagerTrample(t)
	}
	if r.roar > 0 {
		r.roar--
		if r.roar == ravagerRoarAt {
			m.ravagerRoar(t)
		}
	}
	if r.attack > 0 {
		r.attack--
	}
	if r.stun > 0 {
		r.stun--
		if r.stun == 0 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityRavagerRoar, Volume: 1, Pitch: 1})
			r.roar = ravagerRoarTicks
		}
	}
}

// ravagerTrample breaks every leaf block in the body's box grown by 0.2; with
// none to break, a grounded ravager jumps at the wall.
func (m *Mob) ravagerTrample(t *tickView) {
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	broke := false
	for x := int32(math.Floor(minX - ravagerLeafReach)); x <= int32(math.Floor(maxX+ravagerLeafReach)); x++ {
		for y := int32(math.Floor(minY - ravagerLeafReach)); y <= int32(math.Floor(maxY+ravagerLeafReach)); y++ {
			for z := int32(math.Floor(minZ - ravagerLeafReach)); z <= int32(math.Floor(maxZ+ravagerLeafReach)); z++ {
				if IsLeaves(t.w.BlockAt(x, y, z)) {
					t.events = append(t.events, MobEvent{Mob: m, Kind: MobBrokeBlock, X: float64(x), Y: float64(y), Z: float64(z)})
					broke = true
				}
			}
		}
	}
	if !broke && m.OnGround {
		m.jump = true
	}
}

// ravagerRoar hurts every living body within four blocks of the box but
// other ravagers (illagers are only thrown), and throws the mobs among them.
// A player's throw is the client's, off the roar's entity event.
// vanilla: Ravager.roar
func (m *Mob) ravagerRoar(t *tickView) {
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	minX, minY, minZ = minX-ravagerRoarReach, minY-ravagerRoarReach, minZ-ravagerRoarReach
	maxX, maxY, maxZ = maxX+ravagerRoarReach, maxY+ravagerRoarReach, maxZ+ravagerRoarReach
	for i := range t.players {
		p := &t.players[i]
		pw, ph, _ := p.box()
		if p.Dead || p.Spectator ||
			p.X+pw/2 <= minX || p.X-pw/2 >= maxX ||
			p.Z+pw/2 <= minZ || p.Z-pw/2 >= maxZ ||
			p.Y+ph <= minY || p.Y >= maxY {
			continue
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: ravagerRoarDamage, Player: *p, Roar: true})
	}
	for _, o := range t.others {
		if o == m || !o.Alive() || o.Type == TypeRavager {
			continue
		}
		oMinX, oMinY, oMinZ, oMaxX, oMaxY, oMaxZ := o.BlastBox()
		if oMaxX <= minX || oMinX >= maxX || oMaxZ <= minZ || oMinZ >= maxZ || oMaxY <= minY || oMinY >= maxY {
			continue
		}
		vx, vy, vz := RavagerThrow(o.X-m.X, o.Z-m.Z)
		if IsIllagerType(o.Type) {
			o.Push(vx, vy, vz)
			continue
		}
		// The throw lands after the hurt's knockback.
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Damage: ravagerRoarDamage, Player: o.asTarget(), Roar: true, Throw: [3]float64{vx, vy, vz}})
	}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusRavagerRoar},
		MobEvent{Mob: m, Kind: MobActed, X: m.X, Y: m.Y, Z: m.Z})
}

// RavagerThrow is Ravager.strongKnockback: the velocity a body dx, dz from
// the ravager is thrown with, four blocks per tick away from it.
func RavagerThrow(dx, dz float64) (vx, vy, vz float64) {
	d := math.Max(dx*dx+dz*dz, 0.001)
	return dx / d * ravagerThrow, ravagerLift, dz / d * ravagerThrow
}

// RavagerShove is Entity.push(Entity) on a defender dx, dz from the ravager:
// a nudge away from it, nothing within a hundredth of a block.
func RavagerShove(dx, dz float64) (vx, vz float64) {
	d := math.Max(math.Abs(dx), math.Abs(dz))
	if d < 0.01 {
		return 0, 0
	}
	d = math.Sqrt(d)
	return dx / d * math.Min(1/d, 1) * ravagerShove, dz / d * math.Min(1/d, 1) * ravagerShove
}

// RavagerShielded is Ravager.blockedByItem, told a shield took its blow: but
// for a roar in progress, half the time the ravager is stunned, else it
// throws the defender. stunned says which; handled is false for the roar.
func (m *Mob) RavagerShielded() (stunned, handled bool) {
	if m.Type != TypeRavager || m.ravager.roar != 0 {
		return false, false
	}
	if m.Rand.Float64() < ravagerStunOdds {
		m.ravager.stun = ravagerStunTicks
		return true, true
	}
	return false, true
}
