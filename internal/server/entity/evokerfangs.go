package entity

import (
	"slices"

	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// EvokerFangs is the jaws an evoker's spell raises: they wait out a warmup,
// show themselves, bite everything in reach 8 ticks later and vanish.
// vanilla: EvokerFangs

// The fangs' numbers.
// vanilla: EvokerFangs
const (
	// FangsWidth and FangsHeight are the box.
	FangsWidth  = 0.5
	FangsHeight = 0.8
	// FangsTracking and FangsUpdateInterval are the type's clientTrackingRange
	// in chunks and updateInterval.
	FangsTracking       = 6
	FangsUpdateInterval = 2
	// FangsDamage is the bite's hurt.
	FangsDamage = 6.0
	// fangsLife is lifeTicks, counted down after the warmup.
	fangsLife = 22
	// fangsBiteAt is the warmup count the bite lands on.
	fangsBiteAt = -8
	// FangsReach is the box's inflation on each horizontal side.
	FangsReach = 0.2
)

// EvokerFangs is one fangs entity.
type EvokerFangs struct {
	ID int32
	Place
	// Yaw is the facing in degrees.
	Yaw     float32
	OwnerID int32
	Dead    bool

	warmup    int
	life      int
	sentSpike bool
}

// NewEvokerFangs stands fangs at the spot, facing the angle in radians, that
// bite after the warmup in ticks.
func NewEvokerFangs(id int32, x, y, z, radians float64, warmup int, ownerID int32) *EvokerFangs {
	return &EvokerFangs{ID: id, X: x, Y: y, Z: z, Yaw: float32(radians * 57.295776), OwnerID: ownerID,
		warmup: warmup, life: fangsLife}
}

// Warmup is the remaining delay saved by EvokerFangs.addAdditionalSaveData.
func (f *EvokerFangs) Warmup() int { return f.warmup }

// SetLife changes the ticks left after the warmup, for a test that expires the
// fangs on the tick they bite.
func (f *EvokerFangs) SetLife(ticks int) { f.life = ticks }

// SetWarmup changes the saved delay without restarting the attack animation.
func (f *EvokerFangs) SetWarmup(ticks int) { f.warmup = ticks }

// Tick runs one tick. spike is the entity event that starts the client's
// animation, bite the tick the jaws close.
// vanilla: EvokerFangs.tick
func (f *EvokerFangs) Tick() (spike, bite bool) {
	f.warmup--
	if f.warmup >= 0 {
		return false, false
	}
	bite = f.warmup == fangsBiteAt
	if !f.sentSpike {
		f.sentSpike = true
		spike = true
	}
	f.life--
	if f.life < 0 {
		f.Dead = true
	}
	return spike, bite
}

// BiteBox is the box the bite looks for living bodies in.
func (f *EvokerFangs) BiteBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := FangsWidth/2 + FangsReach
	return f.X - half, f.Y, f.Z - half, f.X + half, f.Y + FangsHeight, f.Z + half
}

// AllianceMember identifies a mob or an online player and their effective team.
type AllianceMember struct {
	Mob    *Mob
	Player uuid.UUID
	Team   string
}

// EvokerFangsDirectAlly checks alliances an evoker can resolve before building the victim's box.
// An evoker cannot have an owner, so a vex that names it already has its root.
// vanilla: Evoker.considersEntityAsAlly
func (m *Mob) EvokerFangsDirectAlly(victim *Mob, ownerID uuid.UUID) bool {
	return m.Type == TypeEvoker && (teamOrIllagerAlly(m.Team, victim.Team, true, victim.Type) ||
		victim.Type == TypeVex && ownerID != uuid.Nil && victim.VexOwnerUUID() == ownerID)
}

// FangsAllied checks the bite's alliance in both directions.
// vanilla: EvokerFangs.dealDamageTo, Entity.isAlliedTo
func FangsAllied(owner, victim AllianceMember, lookup func(uuid.UUID) AllianceMember) bool {
	if owner == (AllianceMember{}) || victim == (AllianceMember{}) {
		return false
	}
	return sameAllianceMember(owner, victim) || owner.considersAlly(victim, lookup) || victim.considersAlly(owner, lookup)
}

func sameAllianceMember(a, b AllianceMember) bool {
	return a.Mob != nil && a.Mob == b.Mob || a.Player != uuid.Nil && a.Player == b.Player
}

// vanilla: TamableAnimal.considersEntityAsAlly, Evoker.considersEntityAsAlly, Allay.considersEntityAsAlly
func (a AllianceMember) considersAlly(b AllianceMember, lookup func(uuid.UUID) AllianceMember) bool {
	if a.Mob != nil && a.Mob.Tamable.Tame {
		if root := a.rootOwner(lookup); root != (AllianceMember{}) {
			a = root
		}
	}
	if sameAllianceMember(a, b) || a.teamOrIllagerAlly(b) {
		return true
	}
	if a.Mob == nil {
		return false
	}
	if a.Mob.Type == TypeEvoker && b.Mob != nil && b.Mob.Type == TypeVex {
		root := b.rootOwner(lookup)
		return root != (AllianceMember{}) && (sameAllianceMember(a, root) || a.teamOrIllagerAlly(root))
	}
	return a.Mob.AllayLikesPlayer(b.Player)
}

// vanilla: Entity.considersEntityAsAlly, AbstractIllager.considersEntityAsAlly
func (a AllianceMember) teamOrIllagerAlly(b AllianceMember) bool {
	kind := registry.EntityTypePlayer
	if b.Mob != nil {
		kind = b.Mob.Type
	}
	return teamOrIllagerAlly(a.Team, b.Team, a.Mob != nil && IsIllagerType(a.Mob.Type), kind)
}

func teamOrIllagerAlly(aTeam, bTeam string, illager bool, kind MobType) bool {
	if aTeam != "" || bTeam != "" {
		return aTeam != "" && aTeam == bTeam
	}
	return illager && kind.InTag(registry.EntityTypeTagIDIllagerFriends)
}

// vanilla: OwnableEntity.getRootOwner
func (a AllianceMember) rootOwner(lookup func(uuid.UUID) AllianceMember) AllianceMember {
	var seen []*Mob
	for a.Mob != nil {
		id, ownable := a.Mob.ownerReference()
		if !ownable {
			break
		}
		if id == uuid.Nil || slices.Contains(seen, a.Mob) {
			return AllianceMember{}
		}
		seen = append(seen, a.Mob)
		a = lookup(id)
	}
	return a
}

func (m *Mob) ownerReference() (uuid.UUID, bool) {
	switch {
	case m.Type == TypeVex:
		return m.VexOwnerUUID(), true
	case m.Type == TypeWolf || m.Type == TypeCat || m.Type == TypeParrot || IsNautilus(m.Type):
		return m.Tamable.Owner, true
	case IsEquine(m.Type):
		return m.Horse.Owner, true
	default:
		return uuid.Nil, false
	}
}

// BlastBox is the fangs' own box, what a piston's sweep meets.
func (f *EvokerFangs) BlastBox() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := FangsWidth / 2
	return f.X - half, f.Y, f.Z - half, f.X + half, f.Y + FangsHeight, f.Z + half
}
