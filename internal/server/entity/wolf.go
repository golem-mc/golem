package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// WolfDefaultCollar is the colour a collar starts as.
// vanilla: Wolf.DEFAULT_COLLAR_COLOR
const WolfDefaultCollar = "red"

// wolfFoodHeal is the health a meal restores when the item has no food
// component.
// vanilla: Wolf.mobInteract's feed(player, hand, itemStack, 2.0f, 2.0f)
const wolfFoodHeal = 2.0

// wolfGround marks the wolves_spawnable_on tag.
var wolfGround = registry.BlockTagOf("minecraft:wolves_spawnable_on")

// finalizeWolfVariant gives the wolf its pack's variant, the first member
// choosing it by the biome for the rest.
// vanilla: Wolf.finalizeSpawn
func (m *Mob) finalizeWolfVariant(dice Dice, biome registry.Biome, group *SpawnGroup) {
	if group != nil && group.wolfVariant != "" {
		m.Variant = group.wolfVariant
		return
	}
	m.Variant = rules.WolfVariant(biome, dice)
	if group != nil {
		group.wolfVariant = m.Variant
	}
}

// IsWolfFood is the wolf_food item tag.
// vanilla: Wolf.isFood
func IsWolfFood(item int32) bool { return registry.Item(item).InTag(registry.ItemTagIDWolfFood) }

// FeedWolf heals a hurt tame wolf by a meal: twice the stack's nutrition, two
// when it has none. It reports false for a wolf at full health.
// vanilla: Wolf.mobInteract, TamableAnimal.feed
func (m *Mob) FeedWolf(stack protocol.ItemStack) bool {
	if !m.Tamable.Tame || m.Health >= m.healthCap() || !IsWolfFood(stack.ItemID) {
		return false
	}
	m.feedHeal(stack, 2, wolfFoodHeal)
	return true
}

// DyeCollar sets the collar to the colour a dye names and reports whether it
// changed.
// vanilla: Wolf.mobInteract
func (m *Mob) DyeCollar(colour string) bool {
	if colour == "" || colour == m.Collar {
		return false
	}
	m.SetMetadataCollar(colour)
	return true
}

// wolfCanMate is Wolf.canMate: both tame, the partner not sitting, both in
// love.
// vanilla: Wolf.canMate
func (m *Mob) wolfCanMate(partner *Mob) bool {
	return m.Tamable.Tame && partner.Tamable.Tame && !partner.Tamable.SittingPose &&
		m.IsInLove() && partner.IsInLove()
}

// breedWolf is Wolf.getBreedOffspring: a tame mother's pup is tame, owned by
// her owner and wears the parents' collars mixed; either parent's variant.
// vanilla: Wolf.getBreedOffspring
func (m *Mob) breedWolf(baby, partner *Mob) {
	if m.Tamable.Tame {
		baby.SetMetadataTamableTame(true)
		baby.SetMetadataTamableOwner(m.Tamable.Owner)
		baby.MaxHealth = tamedWolfHealth
		baby.SetMetadataHealth(tamedWolfHealth)
		baby.SetMetadataCollar(rules.MixedWoolColour(m.Collar, partner.Collar, m.Rand))
	}
	_, sounds, _ := VariantRegistries(TypeWolf)
	baby.SoundVariant = rules.RandomRegistryEntry(sounds, GoDice{m.Rand})
}

// wolfShakeTicks is a shake's length: Wolf.shakeAnim climbs 0.05 a tick as a
// float32, reaches 2.0 on the 41st, and the shake ends on the 42nd when
// shakeAnimO catches up.
const wolfShakeTicks = 42

// wolfShake is a wolf's wetness and shake clock.
type wolfShake struct {
	wet     bool
	shaking bool
	ticks   int
}

// wolfWetStep wets the wolf in water or rain and shakes it dry once it stands
// on the ground, off its path and out of the wet.
// vanilla: Wolf.aiStep, Wolf.tick
func (m *Mob) wolfWetStep(w World, events []MobEvent) []MobEvent {
	if m.Dead {
		return events
	}
	s := &m.wolf
	if s.wet && !s.shaking && len(m.nav.path) == 0 && m.OnGround {
		s.shaking, s.ticks = true, 0
		events = append(events, MobEvent{Mob: m, Kind: MobWolfShook})
	}
	if m.InWater() || rules.RainsOnBody(w, m.X, m.Y, m.Z, m.Y+m.height()) {
		s.wet = true
		if s.shaking {
			s.shaking, s.ticks = false, 0
			events = append(events, MobEvent{Mob: m, Kind: MobWolfShakeCancelled})
		}
		return events
	}
	if s.shaking {
		if s.ticks == 0 {
			pitch := float32((m.Rand.Float64()-m.Rand.Float64())*0.2 + 1)
			events = append(events, MobEvent{Mob: m, Kind: MobWolfShakeSound, Pitch: pitch}, MobEvent{Mob: m, Kind: MobEntityAction})
		}
		s.ticks++
		if s.ticks >= wolfShakeTicks {
			*s = wolfShake{}
		}
	}
	return events
}

// wolfPrey is Wolf.PREY_SELECTOR; the fox is matched by name.
func wolfPrey(o *Mob) bool {
	return o.Type == TypeSheep || o.Type == TypeRabbit || o.Type == registry.EntityTypeFox
}

// isSkeletonKind is AbstractSkeleton.
func isSkeletonKind(o *Mob) bool {
	switch o.Type {
	case TypeSkeleton, TypeStray, TypeBogged, TypeParched, TypeWitherSkeleton:
		return true
	}
	return false
}

// The wolf's goal priorities.
// vanilla: Wolf.registerGoals
const (
	wolfPriorityFloat  = 1
	wolfPanicPace      = 1.5
	wolfPriorityAvoid  = 3
	wolfPriorityLeap   = 4
	wolfPriorityMelee  = 5
	wolfPriorityBreed  = 7
	wolfPriorityStroll = 8
	wolfPriorityLook   = 10
	wolfPriorityHurtBy = 3
)

// wolfGoals wires float, the environmental-damage panic (fire, lava, cactus,
// lightning), the owner's quarrels, sitting, leap and bite at an attacker
// (the pack sharing the grudge), following the owner, breeding, stroll and
// two glances, begging, the prey hunts and llama avoidance.
// vanilla: Wolf.registerGoals
func wolfGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&panicGoal{m: m, tame: true, pace: wolfPanicPace, causes: panicEnvironmentalCauses},
		&ownerTargetGoal{m: m, priority: wolfPriorityOwnerHurtBy, byAttacker: true},
		&ownerTargetGoal{m: m, priority: wolfPriorityOwnerHurt},
		&sitGoal{m: m},
		&hurtByGoal{m: m, priority: wolfPriorityHurtBy, alertOthers: true},
		newWolfAvoidGoal(m),
		&leapAtTargetGoal{m: m, priority: wolfPriorityLeap},
		&nearestTargetGoal{m: m, priority: 4, accept: func(t *tickView, p *Player) bool { return m.angryAt(t.w, p) }},
		&meleeAttackGoal{m: m, priority: wolfPriorityMelee, followUnseen: true},
		&mobTargetGoal{m: m, priority: 5, wildOnly: true, unsighted: true, matches: wolfPrey},
		&followOwnerGoal{m: m},
		&mobTargetGoal{m: m, priority: 6, wildOnly: true, unsighted: true, matches: func(o *Mob) bool {
			return o.Type == TypeTurtle && o.IsBaby() && !o.InWater()
		}},
		&breedGoal{m: m, priority: wolfPriorityBreed},
		&mobTargetGoal{m: m, priority: 7, unsighted: true, matches: isSkeletonKind},
		&randomStroll{m: m, priority: wolfPriorityStroll},
		&resetAngerGoal{m: m, alertSame: true, priority: 8},
		&begGoal{m: m},
		&lookAtPlayer{m: m, priority: wolfPriorityLook},
		&randomLookAround{m: m, priority: wolfPriorityLook},
	}
}

// wolfAvoidGoal adds the strength roll and target clearing to llama avoidance.
// vanilla: Wolf.WolfAvoidEntityGoal
type wolfAvoidGoal struct{ avoidGoal }

// newWolfAvoidGoal uses Wolf.registerGoals' distance and paces.
func newWolfAvoidGoal(m *Mob) *wolfAvoidGoal {
	g := &wolfAvoidGoal{avoidGoal{m: m, priority: wolfPriorityAvoid,
		active: func() bool { return true }, maxDist: 24, walk: 1.5, sprint: 1.5,
		mobs: []MobType{TypeLlama, TypeTraderLlama}}}
	g.canFlee = g.avoidLlama
	return g
}

// avoidLlama rolls only after the nearest llama has a reachable escape path.
// vanilla: Wolf.WolfAvoidEntityGoal.canUse, avoidLlama
func (g *wolfAvoidGoal) avoidLlama(llama *Mob) bool {
	return !g.m.Tamable.Tame && llama.Horse.Strength >= int32(g.m.Rand.Intn(5))
}

func (g *wolfAvoidGoal) Start(t *tickView) {
	g.m.SetMetadataTargetID(0)
	g.avoidGoal.Start(t)
}

func (g *wolfAvoidGoal) Tick(t *tickView) {
	g.m.SetMetadataTargetID(0)
	g.avoidGoal.Tick(t)
}
