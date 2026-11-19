package entity

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypePanda is minecraft:panda.
const TypePanda = registry.EntityTypePanda

func init() {
	// vanilla: EntityTypes.PANDA, Panda.createAttributes, BABY_DIMENSIONS
	kinds[TypePanda] = &kind{
		fireAverse: true,
		name:       "Panda", width: float64(float32(1.3)), height: float64(float32(1.25)), eyeHeight: float64(float32(1.25) * 0.85),
		babyWidth: float64(float32(1.3) * float32(0.5)), babyHeight: float64(float32(1.25) * float32(0.5)), babyEye: 0.28125,
		babyable: true, maxHealth: 20, speed: float64(float32(0.15)), attackDamage: 6,
		temptTag: &registry.ItemTagIDPandaFood, temptPace: 1, followPace: 1.25, strollPace: 1,
		sounds:          MobSounds{Ambient: registry.SoundEntityPandaAmbient, Hurt: registry.SoundEntityPandaHurt, Death: registry.SoundEntityPandaDeath, Eat: registry.SoundEntityPandaEat, Step: registry.SoundEntityPandaStep},
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	}
	mobTracking[TypePanda] = tracking{rangeChunks: 10, updateInterval: 3}
}

// PandaGene is Panda.Gene; the value is its id.
type PandaGene uint8

// vanilla: Panda.Gene
const (
	GeneNormal PandaGene = iota
	GeneLazy
	GeneWorried
	GenePlayful
	GeneBrown
	GeneWeak
	GeneAggressive
)

var pandaGeneNames = [...]string{"normal", "lazy", "worried", "playful", "brown", "weak", "aggressive"}

// Name is the gene's serialized name.
func (g PandaGene) Name() string { return pandaGeneNames[g] }

func (g PandaGene) recessive() bool { return g == GeneBrown || g == GeneWeak }

// pandaGeneNamed is Gene.CODEC; an unknown name reads as normal.
func pandaGeneNamed(name string) PandaGene {
	for i, n := range pandaGeneNames {
		if n == name {
			return PandaGene(i)
		}
	}
	return GeneNormal
}

// pandaGeneVariant is Gene.getVariantFromGenes: a recessive main gene shows
// only when the hidden gene matches.
func pandaGeneVariant(main, hidden PandaGene) PandaGene {
	if main.recessive() && main != hidden {
		return GeneNormal
	}
	return main
}

// pandaRandomGene is Gene.getRandom.
func pandaRandomGene(roll int) PandaGene {
	switch {
	case roll == 0:
		return GeneLazy
	case roll == 1:
		return GeneWorried
	case roll == 2:
		return GenePlayful
	case roll == 4:
		return GeneAggressive
	case roll < 9:
		return GeneWeak
	case roll < 11:
		return GeneBrown
	}
	return GeneNormal
}

// pandaState is the panda's own state.
type pandaState struct {
	main, hidden PandaGene
	// unhappy is UNHAPPY_COUNTER; breedRetry is the Age at which the breed goal
	// may set it again.
	unhappy, breedRetry int
	// flags is DATA_ID_FLAGS; sneeze and eat are SNEEZE_COUNTER and EAT_COUNTER.
	flags     uint8
	sneeze    int
	eat       int
	rollCount int
	rollX     float64
	rollZ     float64
	gotBamboo bool
	didBite   bool
	// scared is isScared as of this tick, for the interaction between ticks.
	scared bool
	// sitAfter and backAfter are the Age before which the sit and lie-on-back
	// goals do not start again.
	sitAfter  int
	backAfter int
	// forceSneeze makes the sneeze goal start at its next check, for tests.
	forceSneeze bool
	// ground is the tick's view of the items lying about.
	ground []FoxItem
}

// The panda's numbers.
// vanilla: Panda.TOTAL_UNHAPPY_TIME, PandaBreedGoal.canUse
const (
	pandaUnhappyTime  = 32
	pandaUnhappyRetry = 600
)

// The panda's flag bits.
// vanilla: Panda.FLAG_SNEEZE, FLAG_ROLL, FLAG_SIT, FLAG_ON_BACK
const (
	pandaFlagSneeze uint8 = 2
	pandaFlagRoll   uint8 = 4
	pandaFlagSit    uint8 = 8
	pandaFlagOnBack uint8 = 16
)

// vanilla: Panda.registerGoals; the breed, tempt, look, follow and stroll
// goals keep the shared priorities.
const (
	pandaPriorityAttack = 3
	pandaPriorityAvoid  = 6
	pandaPrioritySit    = 7
	pandaPriorityLie    = 8
	pandaPrioritySneeze = 8
	pandaPriorityRoll   = 12
)

func pandaGoals(m *Mob) []Goal {
	// vanilla: Panda.registerGoals, PandaPanicGoal, PandaAvoidGoal, PandaLookAtPlayerGoal
	avoid := func(monsters bool, dist float64) Goal {
		return &avoidGoal{m: m, priority: pandaPriorityAvoid, maxDist: dist, walk: 2, sprint: 2, monsters: monsters,
			active: func() bool { return m.pandaVariant() == GeneWorried && m.pandaCanAct() }}
	}
	return []Goal{newFloatGoal(m),
		&gatedGoal{Goal: &panicGoal{m: m, pace: 2, causes: panicEnvironmentalCauses}, enabled: m.pandaNotSitting},
		&hurtByGoal{m: m, priority: 1, alertOthers: true,
			alertTo: func(o *Mob) bool { return o.Type == TypePanda && o.pandaVariant() == GeneAggressive },
			drop:    func() bool { return m.panda.gotBamboo || m.panda.didBite }},
		&breedGoal{m: m, priority: 2},
		&meleeAttackGoal{m: m, priority: pandaPriorityAttack, followUnseen: true, pace: float64(float32(1.2)),
			swingSound: registry.SoundEntityPandaBite, refuse: func() bool { return !m.pandaCanAct() },
			onSwing: func() { m.panda.didBite = m.pandaVariant() != GeneAggressive }},
		&temptGoal{m: m, priority: 4},
		avoid(false, 8), avoid(true, 4),
		&pandaSitGoal{m: m},
		&pandaLieGoal{m: m},
		&pandaSneezeGoal{pandaGoalBase{m}},
		&gatedGoal{Goal: &lookAtPlayer{m: m, priority: 9, reach: 6}, enabled: m.pandaCanAct},
		&randomLookAround{m: m, priority: 10},
		&pandaRollGoal{pandaGoalBase{m}},
		&followParentGoal{m: m, priority: 13},
		&randomStroll{m: m, priority: 14}}
}

// PandaMainGene, PandaHiddenGene and PandaUnhappy are the synched MAIN_GENE_ID,
// HIDDEN_GENE_ID and UNHAPPY_COUNTER.
func (m *Mob) PandaMainGene() PandaGene   { return m.panda.main }
func (m *Mob) PandaHiddenGene() PandaGene { return m.panda.hidden }
func (m *Mob) PandaUnhappy() int          { return m.panda.unhappy }

func (m *Mob) pandaVariant() PandaGene { return pandaGeneVariant(m.panda.main, m.panda.hidden) }

// setPandaAttributes is Panda.setAttributes on the base values the kind starts
// from: a weak variant has 10 health, a lazy one walks at 0.07.
func (m *Mob) setPandaAttributes() {
	m.MaxHealth = m.kind().maxHealth
	if m.pandaVariant() == GeneWeak {
		m.MaxHealth = 10
	}
	m.SetMetadataHealth(min(m.Health, m.healthCap()))
}

// finalizePanda is Panda.finalizeSpawn: main gene, then hidden gene.
func (m *Mob) finalizePanda(dice Dice) {
	m.SetMetadataPandaMain(pandaRandomGene(int(dice.NextIntN(16))))
	m.SetMetadataPandaHidden(pandaRandomGene(int(dice.NextIntN(16))))
	m.setPandaAttributes()
}

// breedPanda is Panda.getBreedOffspring: Panda.setGeneFromParents with this
// panda and the partner as the parents.
func (m *Mob) breedPanda(baby, partner *Mob) {
	if baby.Rand.Intn(2) == 1 {
		baby.SetMetadataPandaMain(m.pandaOneOfGenes())
		baby.SetMetadataPandaHidden(partner.pandaOneOfGenes())
	} else {
		baby.SetMetadataPandaMain(partner.pandaOneOfGenes())
		baby.SetMetadataPandaHidden(m.pandaOneOfGenes())
	}
	if baby.Rand.Intn(32) == 0 {
		baby.SetMetadataPandaMain(pandaRandomGene(baby.Rand.Intn(16)))
	}
	if baby.Rand.Intn(32) == 0 {
		baby.SetMetadataPandaHidden(pandaRandomGene(baby.Rand.Intn(16)))
	}
	baby.setPandaAttributes()
}

// pandaOneOfGenes is Panda.getOneOfGenesRandomly.
func (m *Mob) pandaOneOfGenes() PandaGene {
	if m.Rand.Intn(2) == 1 {
		return m.panda.main
	}
	return m.panda.hidden
}

// pandaMayBreed is the rest of PandaBreedGoal.canUse once a partner is found:
// with no bamboo near, the panda turns unhappy instead of courting.
func (m *Mob) pandaMayBreed(t *tickView) bool {
	if m.panda.unhappy != 0 {
		return false
	}
	if m.pandaFindsBamboo(t.w) {
		return true
	}
	if m.panda.breedRetry <= m.Age {
		m.SetMetadataPandaUnhappy(pandaUnhappyTime)
		m.panda.breedRetry = m.Age + pandaUnhappyRetry
	}
	return false
}

// pandaFindsBamboo is PandaBreedGoal.canFindBamboo: a spiral of rings out to
// radius 7 on each of the three layers from the feet up.
func (m *Mob) pandaFindsBamboo(w World) bool {
	bx, by, bz := floorI(m.X), floorI(m.Y), floorI(m.Z)
	for y := range int32(3) {
		for r := range int32(8) {
			for x := int32(0); x <= r; x = next(x) {
				z := int32(0)
				if x < r && x > -r {
					z = r
				}
				for ; z <= r; z = next(z) {
					if registry.BlockOf(w.BlockAt(bx+x, by+y, bz+z)) == registry.BlockBamboo {
						return true
					}
				}
			}
		}
	}
	return false
}

// next is the spiral's step 0, 1, -1, 2, -2 (vanilla's `x > 0 ? -x : 1 - x`).
func next(v int32) int32 {
	if v > 0 {
		return -v
	}
	return 1 - v
}

// pandaFields reads the genes before the saved attributes and Health, so
// those load onto the genes' bases.
func (m *Mob) pandaFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	if m.Type != TypePanda {
		return
	}
	if load {
		name, _ := nbt.Get[nbt.String](tag, "MainGene")
		m.SetMetadataPandaMain(pandaGeneNamed(string(name)))
		name, _ = nbt.Get[nbt.String](tag, "HiddenGene")
		m.SetMetadataPandaHidden(pandaGeneNamed(string(name)))
		m.setPandaAttributes()
		return
	}
	out.Set("MainGene", nbt.String(m.panda.main.Name()))
	out.Set("HiddenGene", nbt.String(m.panda.hidden.Name()))
}
