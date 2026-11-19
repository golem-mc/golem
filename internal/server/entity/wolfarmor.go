package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

const (
	// wolfRepairFraction is the 0.125 of the maximum damage one scute mends.
	wolfRepairFraction = 0.125
	// wolfWhineHealth is the health below which a tame wolf whines.
	wolfWhineHealth = 20
)

// The wolf armor's crack thresholds, as fractions of durability left.
// vanilla: Crackiness.WOLF_ARMOR
var wolfCrackFractions = [3]float32{0.95, 0.69, 0.32}

// WearsWolfArmor reports whether the body slot holds a wolf armor.
func (m *Mob) WearsWolfArmor() bool {
	return !m.Body.Empty() && registry.Item(m.Body.ItemID) == registry.ItemWolfArmor
}

// EquipWolfArmor puts one wolf armor on a grown tame wolf of the player's
// whose body is bare, and reports whether it did.
// vanilla: Wolf.mobInteract
func (m *Mob) EquipWolfArmor(stack protocol.ItemStack, owner bool) bool {
	if registry.Item(stack.ItemID) != registry.ItemWolfArmor || !m.Body.Empty() || !owner || m.IsBaby() {
		return false
	}
	stack.Count = 1
	m.SetEquipment(6, stack)
	return true
}

// RepairWolfArmor spends a scute on a sitting wolf's armour, removing an
// eighth of max damage. It reports whether the item was a valid repair for
// damaged armour.
// vanilla: Wolf.mobInteract
func (m *Mob) RepairWolfArmor(item int32, owner bool) bool {
	if !m.Tamable.SittingPose || !m.WearsWolfArmor() || !owner || m.Body.Damage <= 0 ||
		!registry.StackRepairs(m.Body, item) {
		return false
	}
	unit := int32(float32(registry.StackMaxDamage(m.Body)) * wolfRepairFraction)
	stack := m.Body
	stack.Damage = max(0, stack.Damage-unit)
	m.SetEquipment(6, stack)
	return true
}

// armorCrackLevel is Crackiness.byDamage: 0 for none up to 3 for high.
func armorCrackLevel(s protocol.ItemStack) int {
	limit := registry.StackMaxDamage(s)
	if s.Empty() || limit <= 0 {
		return 0
	}
	fraction := float32(limit-s.Damage) / float32(limit)
	switch {
	case fraction < wolfCrackFractions[2]:
		return 3
	case fraction < wolfCrackFractions[1]:
		return 2
	case fraction < wolfCrackFractions[0]:
		return 1
	}
	return 0
}

// wolfArmorTakes is Wolf.actuallyHurt: body armour soaks a whole blow,
// wearing by the damage rounded up, unless the type bypasses it. It reports
// whether the armour took the blow (health unchanged).
// vanilla: Wolf.canArmorAbsorb, actuallyHurt
func (m *Mob) wolfArmorTakes(amount float64, damageType registry.DamageType) bool {
	if m.Type != TypeWolf || !m.WearsWolfArmor() || damageType.InTag(registry.DamageTypeTagIDBypassesWolfArmor) {
		return false
	}
	before := armorCrackLevel(m.Body)
	worn, broke := rules.Wear(m.Body, int32(math.Ceil(amount)))
	m.SetEquipment(6, worn)
	m.armorCracked = m.armorCracked || armorCrackLevel(worn) != before
	m.armorBroke = m.armorBroke || broke
	return true
}

// wolfArmorStep turns the crack and the break a blow left into their events.
func (m *Mob) wolfArmorStep(events []MobEvent) []MobEvent {
	if m.armorCracked {
		events = append(events, MobEvent{Mob: m, Kind: MobWolfArmorCracked})
	}
	if m.armorBroke {
		events = append(events, MobEvent{Mob: m, Kind: MobWolfArmorBroke})
	}
	m.armorCracked, m.armorBroke = false, false
	return events
}

type wolfSoundSet struct{ ambient, hurt, death, growl, whine, pant registry.Sound }

// wolfSoundSet selects the adult variant or the shared baby sounds.
// vanilla: Wolf.getSoundSet, SoundEvents.registerWolfSoundVariants
func (m *Mob) wolfSoundSet() wolfSoundSet {
	if m.IsBaby() {
		return wolfSoundsBaby
	}
	switch m.SoundVariant {
	case "minecraft:puglin":
		return wolfSoundsPuglin
	case "minecraft:sad":
		return wolfSoundsSad
	case "minecraft:angry":
		return wolfSoundsAngry
	case "minecraft:grumpy":
		return wolfSoundsGrumpy
	case "minecraft:big":
		return wolfSoundsBig
	case "minecraft:cute":
		return wolfSoundsCute
	default:
		return wolfSoundsClassic
	}
}

var wolfSoundsClassic = wolfSoundSet{registry.SoundEntityWolfAmbient, registry.SoundEntityWolfHurt, registry.SoundEntityWolfDeath, registry.SoundEntityWolfGrowl, registry.SoundEntityWolfWhine, registry.SoundEntityWolfPant}
var wolfSoundsBaby = wolfSoundSet{registry.SoundEntityBabyWolfAmbient, registry.SoundEntityBabyWolfHurt, registry.SoundEntityBabyWolfDeath, registry.SoundEntityBabyWolfGrowl, registry.SoundEntityBabyWolfWhine, registry.SoundEntityBabyWolfPant}
var wolfSoundsPuglin = wolfSoundSet{registry.SoundEntityWolfPuglinAmbient, registry.SoundEntityWolfPuglinHurt, registry.SoundEntityWolfPuglinDeath, registry.SoundEntityWolfPuglinGrowl, registry.SoundEntityWolfPuglinWhine, registry.SoundEntityWolfPuglinPant}
var wolfSoundsSad = wolfSoundSet{registry.SoundEntityWolfSadAmbient, registry.SoundEntityWolfSadHurt, registry.SoundEntityWolfSadDeath, registry.SoundEntityWolfSadGrowl, registry.SoundEntityWolfSadWhine, registry.SoundEntityWolfSadPant}
var wolfSoundsAngry = wolfSoundSet{registry.SoundEntityWolfAngryAmbient, registry.SoundEntityWolfAngryHurt, registry.SoundEntityWolfAngryDeath, registry.SoundEntityWolfAngryGrowl, registry.SoundEntityWolfAngryWhine, registry.SoundEntityWolfAngryPant}
var wolfSoundsGrumpy = wolfSoundSet{registry.SoundEntityWolfGrumpyAmbient, registry.SoundEntityWolfGrumpyHurt, registry.SoundEntityWolfGrumpyDeath, registry.SoundEntityWolfGrumpyGrowl, registry.SoundEntityWolfGrumpyWhine, registry.SoundEntityWolfGrumpyPant}
var wolfSoundsBig = wolfSoundSet{registry.SoundEntityWolfBigAmbient, registry.SoundEntityWolfBigHurt, registry.SoundEntityWolfBigDeath, registry.SoundEntityWolfBigGrowl, registry.SoundEntityWolfBigWhine, registry.SoundEntityWolfBigPant}
var wolfSoundsCute = wolfSoundSet{registry.SoundEntityWolfCuteAmbient, registry.SoundEntityWolfCuteHurt, registry.SoundEntityWolfCuteDeath, registry.SoundEntityWolfCuteGrowl, registry.SoundEntityWolfCuteWhine, registry.SoundEntityWolfCutePant}

// wolfSounds is the set the wolf speaks; armor that takes the blow answers
// a hurt with its own sound.
// vanilla: Wolf.getHurtSound, getDeathSound
func (m *Mob) wolfSounds() MobSounds {
	p := m.wolfSoundSet()
	s := MobSounds{Ambient: p.ambient, Hurt: p.hurt, Death: p.death, Step: registry.SoundEntityWolfStep}
	if m.IsBaby() {
		s.Step = registry.SoundEntityBabyWolfStep
	}
	if m.WearsWolfArmor() && !m.lastDamageType.InTag(registry.DamageTypeTagIDBypassesWolfArmor) {
		s.Hurt = registry.SoundItemWolfArmorDamage
	}
	return s
}

// wolfAmbient picks the idle sound: a growl when angry, else a third of the
// time a whine (a hurt tame wolf) or a pant, else the plain ambient.
// vanilla: Wolf.getAmbientSound
func (m *Mob) wolfAmbient() registry.Sound {
	p := m.wolfSoundSet()
	if m.Angry() {
		return p.growl
	}
	if m.Rand.Intn(3) == 0 {
		if m.Tamable.Tame && m.Health < wolfWhineHealth {
			return p.whine
		}
		return p.pant
	}
	return p.ambient
}

// begGoal is BegGoal: while a player within eight blocks holds a bone or
// wolf food, the wolf tilts its head and watches them for two to four
// seconds.
// vanilla: BegGoal
type begGoal struct {
	m        *Mob
	player   int32
	lookTime int
}

const begRange = 8.0

func (g *begGoal) Priority() int    { return 9 }
func (g *begGoal) Flags() GoalFlags { return GoalLook }

func begInteresting(p *Player) bool {
	for _, s := range [2]protocol.ItemStack{p.HeldItem, p.OffHandItem} {
		if !s.Empty() && (registry.Item(s.ItemID) == registry.ItemBone || IsWolfFood(s.ItemID)) {
			return true
		}
	}
	return false
}

func (g *begGoal) CanStart(t *tickView) bool {
	p := g.m.nearestPlayerWithin(t, begRange, true)
	if p == nil || !begInteresting(p) {
		return false
	}
	g.player = p.EntityID
	return true
}

func (g *begGoal) ShouldContinue(t *tickView) bool {
	p := t.playerByID(g.player)
	return p != nil && !p.Dead && g.m.distanceToPlayer(p) <= begRange && g.lookTime > 0 && begInteresting(p)
}

func (g *begGoal) Start(*tickView) {
	g.m.SetMetadataBegging(true)
	g.lookTime = reducedTickDelay(40 + g.m.Rand.Intn(40))
}

func (g *begGoal) Stop(*tickView) { g.m.SetMetadataBegging(false) }

func (g *begGoal) Tick(t *tickView) {
	g.lookTime--
	if p := t.playerByID(g.player); p != nil {
		dx, dz := p.X-g.m.X, p.Z-g.m.Z
		g.m.HeadYaw = float32(yawToward(dx, dz))
		g.m.Pitch = float32(pitchToward(dx, p.Y+p.eyeHeight()-(g.m.Y+g.m.height()*0.85), dz))
	}
}

func (g *begGoal) RequiresEveryTick() bool { return false }
