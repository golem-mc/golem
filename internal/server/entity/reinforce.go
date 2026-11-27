package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// ZombieTraits are the attribute modifiers Zombie.handleAttributes and
// reinforcement calls leave, stored as summed values.
type ZombieTraits struct {
	// ReinforceBase is the spawn_reinforcements base, rolled up to 0.1;
	// LeaderBonus is the leader's 0.5-0.75 on top; Charge sums the caller and
	// callee -0.05 steps.
	ReinforceBase, LeaderBonus, Charge float64
	// KnockbackResist is the random spawn bonus on knockback_resistance.
	KnockbackResist float64
	// FollowBonus is the ADD_MULTIPLIED_TOTAL amount on follow_range, 0
	// where none was applied.
	FollowBonus float64
	// LeaderHealth is the leader's ADD_MULTIPLIED_TOTAL amount on
	// max_health (1-4), 0 for a body that is no leader.
	LeaderHealth float64
}

// zombieState is a zombie-family body's traits and the call a landed hurt
// waits on.
type zombieState struct {
	ZombieTraits
	// callFor is the entity ID a landed hurt would call a reinforcement
	// against, 0 when no hurt is waiting to roll.
	callFor int32
}

// Traits returns the attribute modifiers the body carries.
func (m *Mob) Traits() ZombieTraits { return m.zombie.ZombieTraits }

// SetTraits replaces the body's attribute modifiers, scaling max_health to
// the leader amount. Health is left as it is, capped at the new maximum.
func (m *Mob) SetTraits(t ZombieTraits) {
	m.MaxHealth = m.MaxHealth / (1 + m.zombie.LeaderHealth) * (1 + t.LeaderHealth)
	m.SetMetadataHealth(math.Min(m.Health, m.MaxHealth))
	m.zombie.ZombieTraits = t
	m.syncMaxHealth()
	m.syncZombieTraits(m.attrs)
}

// syncCharge writes the caller and callee charge through the map, leaving
// the rest of the attribute as a command set it.
func (m *Mob) syncCharge(a *player.Attributes) {
	if a == nil || !m.is(TypeZombie) {
		return
	}
	if c := m.zombie.Charge; c != 0 {
		a.AddModifier(registry.AttributeSpawnReinforcements, player.Modifier{ID: modCallerCharge, Amount: c,
			Operation: registry.AttributeAddValue, Persistent: true})
	} else {
		a.RemoveModifier(registry.AttributeSpawnReinforcements, modCallerCharge)
	}
}

// syncZombieTraits writes the traits through an installed map: the
// reinforcement base and the modifiers the roll and the calls leave, so the
// map is what the reads, the packet and the save go by.
// vanilla: Zombie.handleAttributes, Zombie.hurtServer
func (m *Mob) syncZombieTraits(a *player.Attributes) {
	if a == nil || !m.is(TypeZombie) {
		return
	}
	z := &m.zombie
	mod := func(attr registry.AttributeID, id string, amount float64, op registry.AttributeOperation) {
		if amount != 0 {
			a.AddModifier(attr, player.Modifier{ID: id, Amount: amount, Operation: op, Persistent: true})
		} else {
			a.RemoveModifier(attr, id)
		}
	}
	if z.ReinforceBase != 0 || a.Has(registry.AttributeSpawnReinforcements) && a.Base(registry.AttributeSpawnReinforcements) != 0 {
		a.SetBase(registry.AttributeSpawnReinforcements, z.ReinforceBase)
	}
	mod(registry.AttributeSpawnReinforcements, modLeaderBonus, z.LeaderBonus, registry.AttributeAddValue)
	m.syncCharge(a)
	mod(registry.AttributeKnockbackResistance, modRandomSpawn, z.KnockbackResist, registry.AttributeAddValue)
	mod(registry.AttributeFollowRange, modZombieSpawn, z.FollowBonus, registry.AttributeAddMultipliedTotal)
}

// syncFollowSpawnBonus writes Mob.finalizeSpawn's follow_range bonus through
// an installed map.
func (m *Mob) syncFollowSpawnBonus(a *player.Attributes) {
	if m.followSpawnBonus != 0 {
		a.AddModifier(registry.AttributeFollowRange, player.Modifier{ID: modRandomSpawn, Amount: m.followSpawnBonus,
			Operation: registry.AttributeAddMultipliedBase, Persistent: true})
	}
}

// The numbers handleAttributes and hurtServer roll against.
// vanilla: Zombie.handleAttributes, Zombie.hurtServer
const (
	reinforceBaseMax      = float64(float32(0.1))
	knockbackBonusMax     = float64(float32(0.05))
	followBonusScale      = 1.5
	leaderOdds            = float32(0.05)
	leaderChanceBase      = 0.5
	leaderChanceSpread    = 0.25
	leaderHealthBase      = 1.0
	leaderHealthSpread    = 3.0
	breakDoorOdds         = float32(0.1)
	reinforceCallerCharge = 0.05
	// calleeCharge is Zombie.ZOMBIE_REINFORCEMENT_CALLEE_CHARGE's -0.05f.
	calleeCharge = float64(float32(0.05))

	// ReinforcementAttempts, ReinforcementRangeMin and ReinforcementRangeMax
	// are the site search's tries and the 7 to 40 block offset per axis.
	ReinforcementAttempts = 50
	ReinforcementRangeMin = 7
	ReinforcementRangeMax = 40
)

// attributesSalt derails the attribute rolls' seed from the other finalize
// streams, so they move neither the mob's dice nor its loot.
const attributesSalt = 0x3C6EF372FE94F82A

// RollConversionAttributes rolls the converted zombie body's attributes.
// vanilla: Zombie.convertToZombieType, Zombie.finalizeSpawn
func (m *Mob) RollConversionAttributes(special float32) { m.handleAttributes(special, true) }

// handleAttributes is Zombie.handleAttributes after finalize: reinforcement
// base, knockback resistance, follow range bonus and the leader roll (which
// also enables door opening). A leader starts at full health unless
// converted.
func (m *Mob) handleAttributes(special float32, conversion bool) {
	r := NewDice(m.lootSeed ^ attributesSalt)
	m.zombie.ReinforceBase = r.Float64() * reinforceBaseMax
	if m.Type == TypeZombifiedPiglin {
		// vanilla: ZombifiedPiglin.randomizeReinforcementsChance
		m.zombie.ReinforceBase = 0
	}
	m.zombie.KnockbackResist = r.Float64() * knockbackBonusMax
	if follow := r.Float64() * followBonusScale * float64(special); follow > 1 {
		m.zombie.FollowBonus = follow
	}
	if r.Float32() < special*leaderOdds {
		m.zombie.LeaderBonus = r.Float64()*leaderChanceSpread + leaderChanceBase
		m.zombie.LeaderHealth = r.Float64()*leaderHealthSpread + leaderHealthBase
		m.MaxHealth *= 1 + m.zombie.LeaderHealth
		if !conversion {
			m.SetMetadataHealth(m.MaxHealth)
		}
		m.CanBreakDoors = true
	}
}

// rollBreakDoors is the finalize's CanBreakDoors roll.
// vanilla: Zombie.finalizeSpawn
func (m *Mob) rollBreakDoors(dice Dice, special float32) {
	m.CanBreakDoors = dice.NextFloat() < special*breakDoorOdds
}

// ReinforcementChance is the spawn_reinforcements attribute: base, leader
// bonus and the charges, clamped to its 0 to 1 range.
func (m *Mob) ReinforcementChance() float64 {
	if m.attrs != nil && m.attrs.Has(registry.AttributeSpawnReinforcements) {
		return math.Min(1, math.Max(0, m.attrs.Value(registry.AttributeSpawnReinforcements)))
	}
	z := &m.zombie
	return math.Min(1, math.Max(0, z.ReinforceBase+z.LeaderBonus+z.Charge))
}

// SetReinforcementBase replaces the spawn_reinforcements base value, for
// the tests that need a call to be certain or impossible.
func (m *Mob) SetReinforcementBase(v float64) {
	m.zombie.ReinforceBase = v
	if m.attrs != nil && m.is(TypeZombie) {
		m.attrs.SetBase(registry.AttributeSpawnReinforcements, v)
	}
}

// SetKnockbackResistance replaces the random spawn bonus on
// knockback_resistance, for the tests that need an exact push.
func (m *Mob) SetKnockbackResistance(v float64) {
	m.zombie.KnockbackResist = v
	m.syncZombieTraits(m.attrs)
}

// KnockbackResistance is the kind's base plus a zombie's random spawn bonus
// plus equipment modifiers, clamped to -2..1.
// vanilla: Attributes.KNOCKBACK_RESISTANCE, Zombie.randomizeReinforcementsChance
func (m *Mob) KnockbackResistance() float64 {
	if m.attrs != nil {
		// The map carries the worn pieces' modifiers and a cube's archetype.
		return math.Max(-2, math.Min(1, m.attrs.Value(registry.AttributeKnockbackResistance)))
	}
	if m.Type == TypeSulfurCube {
		return m.sulfurProf().knockbackResist
	}
	gear := rules.EquipmentAttributeValue(m.EnchEquipment(), registry.AttributeKnockbackResistance)
	return math.Max(-2, math.Min(1, m.kind().knockbackResist+m.zombie.KnockbackResist+gear))
}

// noteReinforcement records that a hurt landed on a zombie-family body and
// whom a call would target: its own target, else the attacker.
// vanilla: Zombie.hurtServer
func (m *Mob) noteReinforcement(attacker int32) {
	if m.zombie.callFor != 0 || !m.is(TypeZombie) {
		return
	}
	target := m.TargetID
	if target == 0 {
		target = attacker
	}
	m.zombie.callFor = target
}

// reinforcementStep rolls the pending call once per hurt, on hard only,
// against the attribute. A call returns an event the server answers with a
// site search.
// vanilla: Zombie.hurtServer
func (m *Mob) reinforcementStep(w World, events []MobEvent) []MobEvent {
	target := m.zombie.callFor
	if target == 0 {
		return events
	}
	m.zombie.callFor = 0
	if w.Difficulty() != rules.DifficultyHard || float64(m.Rand.Float32()) >= m.ReinforcementChance() {
		return events
	}
	return append(events, MobEvent{Mob: m, Kind: MobCalledReinforcement, Target: target, Seed: m.Rand.Int63()})
}

// ChargeReinforcement lowers the caller's chance by 0.05 once its call
// found a site.
func (m *Mob) ChargeReinforcement() {
	m.zombie.Charge -= reinforceCallerCharge
	m.syncCharge(m.attrs)
}

// AsReinforcement marks a called body: its target, and the callee's 0.05
// off its chance.
// vanilla: Zombie.hurtServer
func (m *Mob) AsReinforcement(target int32) {
	m.SetMetadataTargetID(target)
	m.zombie.Charge -= calleeCharge
	m.syncCharge(m.attrs)
}

// ReinforcementSpawnArea is the union of spawn boxes ReinforcementSite can test.
func ReinforcementSpawnArea(t MobType, x, y, z int32) AABB {
	k := kindOf(t)
	hw := k.width / 2
	r := float64(ReinforcementRangeMax)
	return NewAABB(
		float64(x)-r-hw, float64(y)-r, float64(z)-r-hw,
		float64(x)+r+hw, float64(y)+r+k.height, float64(z)+r+hw,
	)
}

// ReinforcementSite tries 50 times for a spot 7 to 40 blocks from the caller
// on each axis: a valid spawn position for the type, no player within 7
// blocks, the box clear of blocks, liquid and bodies. false when none fits.
// vanilla: Zombie.hurtServer
func ReinforcementSite(w SpawnWorld, rng *rand.Rand, t MobType, x, y, z int32, players []SpawnPlayer, bodies *SpawnBodies) (sx, sy, sz int32, ok bool) {
	offset := func() int32 {
		return (ReinforcementRangeMin + rng.Int31n(ReinforcementRangeMax-ReinforcementRangeMin+1)) * (rng.Int31n(3) - 1)
	}
	for range ReinforcementAttempts {
		xt, yt, zt := x+offset(), y+offset(), z+offset()
		if !validSpawnPositionAt(w, GoDice{rng}, t, float64(xt), float64(yt), float64(zt), 0, CategoryMonster) {
			continue
		}
		if playerNear(players, xt, yt, zt, 7) || !validPositionAt(w, t, float64(xt), float64(yt), float64(zt), 0, bodies) {
			continue
		}
		return xt, yt, zt, true
	}
	return 0, 0, 0, false
}

// playerNear reports a living non-spectator within radius of the block's
// corner.
// vanilla: EntityGetter.hasNearbyAlivePlayer
func playerNear(players []SpawnPlayer, x, y, z int32, radius float64) bool {
	for i := range players {
		p := &players[i]
		if p.Spectator || p.Dead {
			continue
		}
		if Dist2(p.X, p.Y, p.Z, float64(x), float64(y), float64(z)) < radius*radius {
			return true
		}
	}
	return false
}
