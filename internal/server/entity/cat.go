package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// catKind is the cat: box 0.6 by 0.7, eyes 0.35, ten health, 0.3 pace,
// attack damage 3; the baby is scalable 0.3 by 0.35. Sounds are the classic
// variant's adult set; Sounds swaps in variant, tame and baby sets.
// vanilla: EntityTypes.CAT's builder, Cat.createAttributes
var catKind = &kind{
	name:         "Cat",
	fireAverse:   true,
	width:        0.6,
	height:       0.7,
	eyeHeight:    0.35,
	maxHealth:    10,
	speed:        float64(float32(0.3)),
	attackDamage: 3,
	babyWidth:    0.3,
	babyHeight:   0.35,
	babyEye:      0.34375,
	temptTag:     &registry.ItemTagIDCatFood,
	sounds: MobSounds{
		Ambient: registry.SoundEntityCatStrayAmbient,
		Hurt:    registry.SoundEntityCatHurt,
		Death:   registry.SoundEntityCatDeath,
		Eat:     registry.SoundEntityCatEat,
	},

	panicPace:       1.5, // Cat.registerGoals' TamableAnimalPanicGoal(this, 1.5)
	temptPace:       0.6, // Cat.TEMPT_SPEED_MOD
	strollPace:      0.8, // Cat.WALK_SPEED_MOD
	ambientInterval: 120, // Cat.getAmbientSoundInterval
}

// The cat's goal priorities.
// vanilla: Cat.registerGoals
const (
	catPriorityTempt  = 4
	catPriorityBreed  = 10
	catPriorityStroll = 11
	catPriorityLookAt = 12
	catPriorityHunt   = 1
)

// catGoals wires float, panic, sit, settling by a sleeping owner, the fish
// tempt, fleeing players while wild, beds, following the owner, seats, the
// leap and bite, breeding, stroll and glance, and the hunts of rabbits and
// baby turtles ashore while wild.
// vanilla: Cat.registerGoals
func catGoals(m *Mob) []Goal {
	m.catTempt = &temptGoal{m: m, priority: catPriorityTempt, untamed: true, canScare: true}
	return []Goal{
		newFloatGoal(m),
		&panicGoal{m: m, tame: true},
		&mobTargetGoal{m: m, priority: catPriorityHunt, victim: TypeRabbit, wildOnly: true, unsighted: true},
		&mobTargetGoal{m: m, priority: catPriorityHunt, wildOnly: true, unsighted: true, matches: isBabyTurtleAshore},
		&sitGoal{m: m},
		&catRelaxOnOwnerGoal{m: m},
		m.catTempt,
		newFelineAvoidGoal(m, catPriorityTempt, func() bool { return !m.Tamable.Tame }),
		newCatLieOnBedGoal(m),
		&followOwnerGoal{m: m, stop: 5},
		newCatSitOnBlockGoal(m),
		&leapAtTargetGoal{m: m, priority: catPriorityLeap, yd: catLeapUp},
		&felineAttackGoal{m: m, priority: catPriorityAttack},
		&breedGoal{m: m, priority: catPriorityBreed},
		&randomStroll{m: m, priority: catPriorityStroll},
		&lookAtPlayer{m: m, priority: catPriorityLookAt, reach: 10}, // vanilla: Cat.registerGoals
	}
}

// isBabyTurtleAshore is Turtle.BABY_ON_LAND_SELECTOR.
func isBabyTurtleAshore(o *Mob) bool { return o.Type == TypeTurtle && o.IsBaby() && !o.InWater() }

// catVariants are the cat_variant entries; every one but all_black has an
// unconditional priority-0 spawn condition.
// vanilla: CatVariants.bootstrap
var catVariants = func() []string {
	r, _ := registry.Get("minecraft:cat_variant")
	return r.Entries
}()

// allBlackMoon is the moon brightness all_black's priority-0 condition needs.
// The structure condition (priority 1) is applied by the arrival caller.
// vanilla: CatVariants.bootstrap, MoonBrightnessCheck
const allBlackMoon = 0.9

// finalizeCat rolls the variant among those whose spawn condition holds under
// the moon brightness given, then the sound variant. blackCat is all_black's
// priority-1 structure condition, which leaves it the only candidate.
// vanilla: Cat.finalizeSpawn, VariantUtils.selectVariantToSpawn, Util.getRandomSafe
func (m *Mob) finalizeCat(dice Dice, moon float32, blackCat bool) {
	open := []string{"minecraft:all_black"}
	if !blackCat {
		open = open[:0]
		for _, v := range catVariants {
			if v != "minecraft:all_black" || moon >= allBlackMoon {
				open = append(open, v)
			}
		}
	}
	m.Variant = open[dice.NextIntN(int32(len(open)))]
	m.SoundVariant = rules.RandomRegistryEntry("minecraft:cat_sound_variant", dice)
}

// BreedCat gives a bred kitten one parent's variant and, from a tame
// mother, her owner.
// vanilla: Cat.getBreedOffspring
func (m *Mob) BreedCat(baby, partner *Mob) {
	if m.Rand.Intn(2) == 0 {
		baby.Variant = partner.Variant
	} else {
		baby.Variant = m.Variant
	}
	if m.Tamable.Tame {
		baby.SetMetadataTamableTame(true)
		baby.SetMetadataTamableOwner(m.Tamable.Owner)
		baby.SetMetadataCollar(rules.MixedWoolColour(m.Collar, partner.Collar, m.Rand))
	}
}

// catSoundSet selects the adult variant or the shared baby sounds.
// vanilla: Cat.getSoundSet
func (m *Mob) catSoundSet() catSoundSet {
	if m.IsBaby() {
		return catSoundsBaby
	}
	if m.SoundVariant == "minecraft:royal" {
		return catSoundsRoyal
	}
	return catSoundsClassic
}

type catSoundSet struct{ ambient, hurt, death, eat, strayAmbient, purr, purreow, begForFood, hiss registry.Sound }

var catSoundsClassic = catSoundSet{registry.SoundEntityCatAmbient, registry.SoundEntityCatHurt, registry.SoundEntityCatDeath, registry.SoundEntityCatEat, registry.SoundEntityCatStrayAmbient, registry.SoundEntityCatPurr, registry.SoundEntityCatPurreow, registry.SoundEntityCatBegForFood, registry.SoundEntityCatHiss}
var catSoundsRoyal = catSoundSet{registry.SoundEntityCatRoyalAmbient, registry.SoundEntityCatRoyalHurt, registry.SoundEntityCatRoyalDeath, registry.SoundEntityCatRoyalEat, registry.SoundEntityCatRoyalStrayAmbient, registry.SoundEntityCatRoyalPurr, registry.SoundEntityCatRoyalPurreow, registry.SoundEntityCatRoyalBegForFood, registry.SoundEntityCatRoyalHiss}
var catSoundsBaby = catSoundSet{registry.SoundEntityBabyCatAmbient, registry.SoundEntityBabyCatHurt, registry.SoundEntityBabyCatDeath, registry.SoundEntityBabyCatEat, registry.SoundEntityBabyCatStrayAmbient, registry.SoundEntityBabyCatPurr, registry.SoundEntityBabyCatPurreow, registry.SoundEntityBabyCatBegForFood, registry.SoundEntityBabyCatHiss}

// catSounds is the set the cat's sound variant speaks, with the stray
// ambient while wild; AmbientVoice rolls the tame cat's idle sound.
func (m *Mob) catSounds() MobSounds {
	p := m.catSoundSet()
	s := MobSounds{Hurt: p.hurt, Death: p.death, Eat: p.eat, Ambient: p.ambient}
	if !m.Tamable.Tame {
		s.Ambient = p.strayAmbient
	}
	return s
}

// catAmbient is a tame cat's purr while in love, else a purreow one time in
// four, else its ambient; a wild cat's stray ambient.
// vanilla: Cat.getAmbientSound
func (m *Mob) catAmbient() registry.Sound {
	p := m.catSoundSet()
	switch {
	case !m.Tamable.Tame:
		return p.strayAmbient
	case m.IsInLove():
		return p.purr
	case m.Rand.Intn(4) == 0:
		return p.purreow
	}
	return p.ambient
}

// catTick begs every 100 ticks while a stray is tempted and purrs every 5
// while curled up or settling.
// vanilla: Cat.tick, Cat.handleLieDown
func (m *Mob) catTick(t *tickView) {
	if m.catTempt != nil && m.ai.isRunning(m.catTempt) && !m.Tamable.Tame && m.Age%100 == 0 {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.catSoundSet().begForFood, Volume: 1, Pitch: 1})
	}
	if (m.Lying || m.RelaxStateOne) && m.Age%5 == 0 {
		volume := 0.6 + 0.4*(m.Rand.Float32()-m.Rand.Float32())
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.catSoundSet().purr, Volume: volume, Pitch: 1})
	}
}

// FeedTamed heals a hurt cat by the stack's nutrition, or one health point without food.
// vanilla: Cat.mobInteract, TamableAnimal.feed
func (m *Mob) FeedTamed(stack protocol.ItemStack) bool {
	if m.Health >= m.healthCap() {
		return false
	}
	m.feedHeal(stack, 1, 1)
	return true
}

// felineDespawns is the cat's and ocelot's own removeWhenFarAway: a wild or
// untrusting one older than two minutes.
// vanilla: Cat.removeWhenFarAway, Ocelot.removeWhenFarAway
func (m *Mob) felineDespawns() bool {
	switch m.Type {
	case TypeCat:
		return !m.Tamable.Tame && m.Age > 2400
	case TypeOcelot:
		return !m.Trusting && m.Age > 2400
	}
	return false
}

// felineTemptPace is the 0.6 the crouch is told by.
// vanilla: Cat.customServerAiStep
const felineTemptPace = 0.6

// felineStance sets the crouch and the sprint of a cat or ocelot from the
// pace of the walk it is on.
// vanilla: Cat.customServerAiStep, Ocelot.customServerAiStep
func (m *Mob) felineStance() {
	if m.Type != TypeCat && m.Type != TypeOcelot {
		return
	}
	n := &m.navOf().nav
	walking := len(n.path) > 0
	m.SetMetadataCrouching(walking && n.speedFactor == felineTemptPace)
	m.SetMetadataSprinting(walking && n.speedFactor == felineSprintPace)
}
