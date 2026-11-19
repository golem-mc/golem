package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// The enderman's persistent anger: a provoked enderman stays angry at its
// target for 20 to 39 seconds after losing sight of it, and a hit by a
// player under universal_anger makes it angry at every player.
// vanilla: NeutralMob, ResetUniversalAngerTargetGoal

// The length of the anger timer, UniformInt of TimeUtil.rangeOfSeconds(20, 39).
const (
	angerMinTicks = 20 * 20
	angerMaxTicks = 39 * 20
)

// angerState is NeutralMob's persistent anger.
type angerState struct {
	// ticks is what is left of persistentAngerEndTime, in ticks; 0 is
	// vanilla's NO_ANGER_END_TIME.
	ticks int
	// target is the angered-at entity, 0 for none; anger with no target under
	// universal_anger means every player.
	target int32
	// targetUUID is persistentAngerTarget for a player target, saved and
	// resolved again on load; hasUUID says it is set.
	targetUUID uuid.UUID
	hasUUID    bool
	// playerHits counts player blows taken, standing in for
	// getLastHurtByMobTimestamp in ResetUniversalAngerTargetGoal.
	playerHits int
	// neverAngered says persistentAngerEndTime still holds the 0 a kind with
	// a plain long field starts at, which the save writes instead of -1
	// until anger starts or stops.
	// vanilla: PolarBear.persistentAngerEndTime, IronGolem, EnderMan, ZombifiedPiglin
	neverAngered bool
}

// startsNeverAngered reports whether the kind's end time starts at 0 rather
// than NO_ANGER_END_TIME (wolves and bees).
func startsNeverAngered(t MobType) bool {
	return t == TypePolarBear || t == TypeIronGolem || t == TypeEnderman || t == TypeZombifiedPiglin
}

// startAngerTimer is NeutralMob.startPersistentAngerTimer.
func (m *Mob) startAngerTimer() {
	m.SetMetadataAngerTicks(m.angerFor())
	m.anger.neverAngered = false
}

// angerFor is the 20 to 39 seconds a fresh timer runs.
func (m *Mob) angerFor() int {
	return angerMinTicks + m.Rand.Intn(angerMaxTicks-angerMinTicks+1)
}

// Angry is NeutralMob.isAngry.
func (m *Mob) Angry() bool { return m.hasAnger() && m.anger.ticks > 0 }

// hasAnger reports whether the kind is a NeutralMob here.
func (m *Mob) hasAnger() bool {
	return m.Type == TypeEnderman || m.Type == TypeZombifiedPiglin || m.Type == TypePolarBear || m.Type == TypeWolf || m.Type == TypeBee || m.Type == TypeIronGolem
}

// HasAnger reports whether the kind keeps a persistent anger a save holds.
func (m *Mob) HasAnger() bool { return m.hasAnger() }

// Anger reports the anger left in ticks and the UUID of the player it is
// held against, for the save.
func (m *Mob) Anger() (ticks int, target uuid.UUID, hasTarget bool) {
	a := &m.anger
	return a.ticks, a.targetUUID, a.hasUUID
}

// SetAnger restores a saved anger.
// vanilla: NeutralMob.readPersistentAngerSaveData
func (m *Mob) SetAnger(ticks int, target uuid.UUID, hasTarget bool) {
	a := &m.anger
	a.neverAngered = false
	m.SetMetadataAngerTicks(max(ticks, 0))
	a.target = 0
	a.targetUUID, a.hasUUID = target, hasTarget
}

// AngerEndTime is NeutralMob.getPersistentAngerEndTime: the game tick the
// anger ends, -1 when not running. GameTime is stamped before each tick, so
// it holds still while the timer counts down.
func (m *Mob) AngerEndTime() int64 {
	if !m.hasAnger() || m.anger.ticks <= 0 {
		return -1
	}
	return m.GameTime + int64(m.anger.ticks)
}

// notePlayerHit records a blow by a player for the universal anger reset.
func (m *Mob) notePlayerHit() { m.anger.playerHits++ }

// validAngerPlayer is NeutralMob.isValidPlayerTarget: not creative,
// spectator or peaceful. A dead player still counts; forgive_dead_players
// ends the anger separately.
func validAngerPlayer(w World, p *Player) bool {
	return (p.Huntable || p.Dead) && w.Difficulty() != rules.DifficultyPeaceful
}

// angryAt is NeutralMob.isAngryAt for a player.
func (m *Mob) angryAt(w World, p *Player) bool {
	a := &m.anger
	if a.ticks > 0 && a.target == 0 && validAngerPlayer(w, p) && w.BoolRule(world.RuleUniversalAnger) {
		return true
	}
	return a.target != 0 && a.target == p.EntityID
}

// PreventsRest is Monster.isPreventingPlayerRest for p: a Monster subclass
// blocks sleep, a zombified piglin only while angry at p.
// vanilla: Monster.isPreventingPlayerRest, ZombifiedPiglin.isPreventingPlayerRest
func (m *Mob) PreventsRest(w World, p *Player) bool {
	if m.Type == TypeZombifiedPiglin {
		return m.angryAt(w, p)
	}
	return m.monsterClass()
}

// stopBeingAngry is NeutralMob.stopBeingAngry.
func (m *Mob) stopBeingAngry() {
	a := &m.anger
	m.retaliate = 0
	m.alerted = 0
	m.SetMetadataTargetID(0)
	a.target = 0
	a.hasUUID = false
	m.SetMetadataAngerTicks(0)
	a.neverAngered = false
}

// angerStep is NeutralMob.updatePersistentAnger(level, true), run ahead of
// the goals each tick.
func (m *Mob) angerStep(t *tickView) {
	a := &m.anger
	m.bee.angerStartedNow = false
	if a.ticks > 0 {
		m.SetMetadataAngerTicks(a.ticks - 1)
	}
	if a.target == 0 && a.hasUUID {
		for i := range t.players {
			if t.players[i].UUID == a.targetUUID {
				a.target = t.players[i].EntityID
				break
			}
		}
	}
	held := a.target
	if held != 0 && m.TargetID == held {
		if o := t.mobByID(held); o != nil && !o.Alive() {
			m.stopBeingAngry()
			return
		}
	}
	var targetPlayer *Player
	if m.TargetID != 0 {
		targetPlayer = t.playerByID(m.TargetID)
		if held != m.TargetID {
			a.target = m.TargetID
			a.hasUUID = false
			if targetPlayer != nil {
				a.targetUUID, a.hasUUID = targetPlayer.UUID, true
			}
		}
		// vanilla: the bee's updatePersistentAnger(level, false) starts the
		// timer only for a new target
		if held != m.TargetID || m.Type != TypeBee {
			m.startAngerTimer()
			m.bee.angerStartedNow = true
		}
	}
	targetsValidPlayer := m.Type != TypeBee && targetPlayer != nil && validAngerPlayer(t.w, targetPlayer)
	if held != 0 && a.ticks <= 0 && !targetsValidPlayer {
		m.stopBeingAngry()
		return
	}
	if held == 0 {
		return
	}
	if p := t.playerByID(held); p != nil {
		if !validAngerPlayer(t.w, p) || p.Dead && t.w.BoolRule(world.RuleForgiveDeadPlayers) {
			m.stopBeingAngry()
		}
	}
}

// resetAngerGoal is ResetUniversalAngerTargetGoal: under universal_anger, a
// player's blow drops the target and grudge and restarts the anger against
// every player.
type resetAngerGoal struct {
	m    *Mob
	seen int
	// alertSame also resets the anger of the same-kind mobs in reach.
	alertSame bool
	// priority replaces the default 4 where nonzero.
	priority int
}

func (g *resetAngerGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return 4
}
func (g *resetAngerGoal) Flags() GoalFlags { return 0 }

func (g *resetAngerGoal) CanStart(t *tickView) bool {
	return t.w.BoolRule(world.RuleUniversalAnger) && g.m.anger.playerHits > g.seen
}

// Start is NeutralMob.forgetCurrentTargetAndRefreshUniversalAnger.
func (g *resetAngerGoal) Start(t *tickView) {
	g.seen = g.m.anger.playerHits
	g.m.stopBeingAngry()
	g.m.startAngerTimer()
	if !g.alertSame {
		return
	}
	for _, o := range t.others {
		if o != g.m && o.Type == g.m.Type && o.Alive() && alertReaches(g.m, o) {
			o.stopBeingAngry()
			o.startAngerTimer()
		}
	}
}

func (g *resetAngerGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *resetAngerGoal) Tick(*tickView)                  {}
func (g *resetAngerGoal) Stop(*tickView)                  {}
func (g *resetAngerGoal) RequiresEveryTick() bool         { return false }
