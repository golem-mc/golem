package entity

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// The illusioner: an evoker-like illager with a bow that turns itself
// invisible and blinds its target. Not modelled: the client-side mirror
// images. Not a raid wave member, and it has no spawn egg.
// vanilla: Illusioner

// TypeIllusioner is the illusioner.
const TypeIllusioner = registry.EntityTypeIllusioner

// The illusioner's numbers.
// vanilla: Illusioner.registerGoals, Illusioner.IllusionerMirrorSpellGoal,
// Illusioner.IllusionerBlindnessSpellGoal
const (
	illusionerBowWalk       = 0.5
	illusionerBowInterval   = 20
	illusionerInvisibleFor  = 1200
	illusionerBlindFor      = 400
	illusionerBlindMinLevel = 2 // effective difficulty it must exceed: Difficulty.NORMAL.ordinal()
)

const mobEffectBlindness = registry.MobEffectBlindness

func init() {
	// vanilla: EntityTypes.ILLUSIONER and Illusioner.createAttributes
	kinds[TypeIllusioner] = &kind{
		name: "Illusioner", width: 0.6, height: 1.95, eyeHeight: 1.6575,
		maxHealth: 32, speed: 0.5, hostile: true, followRange: 18,
		rangedInterval: illusionerBowInterval, rangedIntervalHard: illusionerBowInterval,
		sounds: MobSounds{
			Ambient: registry.SoundEntityIllusionerAmbient,
			Hurt:    registry.SoundEntityIllusionerHurt,
			Death:   registry.SoundEntityIllusionerDeath,
		},
	}
	mobTracking[TypeIllusioner] = tracking{rangeChunks: 8, updateInterval: 3}
}

// illusionerGoals wires Illusioner.registerGoals in priority order across both
// selectors.
func illusionerGoals(m *Mob) []Goal {
	return withRaidGoals(m, []Goal{
		newFloatGoal(m),
		&hurtByGoal{m: m, priority: 1, alertOthers: true, ignore: isRaiderType},
		&castingSpellGoal{m: m},
		newObtainBannerGoal(m),
		&nearestTargetGoal{m: m, priority: 2, memory: evokerMemory},
		newCreakingAvoidGoal(m, 3, evokerAvoidRun, 1.2),
		&mobTargetGoal{m: m, priority: 3, unsighted: true, memory: evokerMemory, kinds: abstractVillagerKinds, matches: illagerVillagerTarget},
		&mobTargetGoal{m: m, priority: 3, unsighted: true, memory: evokerMemory, kinds: ironGolemKinds},
		&spellGoal{m: m, priority: 4, spell: mirrorSpell{}},
		&spellGoal{m: m, priority: 5, spell: &blindnessSpell{}},
		&rangedAttackGoal{m: m, priority: 6, walk: illusionerBowWalk},
		&randomStroll{m: m, priority: 8, pace: 0.6},
		&lookAtPlayer{m: m, priority: 9, reach: 3, chance: 1},
		&lookAtMob{m: m, priority: 10},
	})
}

// mirrorSpell is Illusioner.IllusionerMirrorSpellGoal.
type mirrorSpell struct{}

func (mirrorSpell) id() uint8                            { return SpellDisappear }
func (mirrorSpell) warmup() int                          { return 20 }
func (mirrorSpell) castingTime() int                     { return 20 }
func (mirrorSpell) interval() int                        { return 340 }
func (mirrorSpell) prepareSound() registry.Sound         { return registry.SoundEntityIllusionerPrepareMirror }
func (mirrorSpell) stop(*spellGoal)                      {}
func (mirrorSpell) keeps(g *spellGoal, t *tickView) bool { return g.keepsOnTarget(t) }

func (mirrorSpell) canUse(g *spellGoal, t *tickView) bool {
	return g.baseCanUse(t) && !g.m.hasEffect(mobEffectInvisibility)
}

func (mirrorSpell) perform(g *spellGoal, t *tickView) {
	g.m.giveEffect(t, effect.New(mobEffectInvisibility, illusionerInvisibleFor, 0, false, true, true))
}

// blindnessSpell is Illusioner.IllusionerBlindnessSpellGoal.
type blindnessSpell struct {
	// lastTarget is the entity ID of the last target it blinded.
	lastTarget int32
}

func (*blindnessSpell) id() uint8        { return SpellBlindness }
func (*blindnessSpell) warmup() int      { return 20 }
func (*blindnessSpell) castingTime() int { return 20 }
func (*blindnessSpell) interval() int    { return 180 }
func (*blindnessSpell) prepareSound() registry.Sound {
	return registry.SoundEntityIllusionerPrepareBlindness
}
func (*blindnessSpell) stop(*spellGoal)                      {}
func (*blindnessSpell) keeps(g *spellGoal, t *tickView) bool { return g.keepsOnTarget(t) }

// canUse needs a new target and an effective difficulty above normal.
func (s *blindnessSpell) canUse(g *spellGoal, t *tickView) bool {
	return g.baseCanUse(t) && g.m.TargetID != s.lastTarget && g.m.Effective > illusionerBlindMinLevel
}

func (s *blindnessSpell) start(g *spellGoal) {
	if g.m.TargetID != 0 {
		s.lastTarget = g.m.TargetID
	}
}

func (*blindnessSpell) perform(g *spellGoal, t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	if o := t.mobByID(p.EntityID); o != nil {
		o.giveEffect(t, effect.New(mobEffectBlindness, illusionerBlindFor, 0, false, true, true))
		return
	}
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobBlinded, Player: *p})
}

// giveEffect adds an effect to the body and reports the change.
func (m *Mob) giveEffect(t *tickView, in effect.Instance) {
	if change, ok := m.AddEffect(in); ok {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobEffectChanged, EffectChange: change})
	}
}
