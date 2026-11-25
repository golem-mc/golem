package entity

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// livingAttributes is LivingEntity.createLivingAttributes.
var livingAttributes = []string{
	"max_health", "knockback_resistance", "movement_speed", "armor", "armor_toughness",
	"max_absorption", "step_height", "scale", "gravity", "safe_fall_distance",
	"fall_damage_multiplier", "jump_strength", "entity_interaction_range", "oxygen_bonus",
	"burning_time", "explosion_knockback_resistance", "water_movement_efficiency",
	"movement_efficiency", "attack_knockback", "camera_distance", "waypoint_transmit_range",
	"bounciness", "air_drag_modifier", "friction_modifier", "name_tag_distance",
	"below_name_distance",
}

// supplierKey is the inputs of a mob's supplier; mobs that agree share one.
type supplierKey struct {
	t                MobType
	size             int
	speed, maxHealth float64
	killerBunny      bool
}

var supplierCache sync.Map // supplierKey -> *player.Supplier; read-only once stored

// AttributeSupplier is the attribute set and bases of the mob's kind: the
// living attributes plus the kind's pace base and its own.
// vanilla: Mob.createMobAttributes, Animal.createAnimalAttributes,
// Monster.createMonsterAttributes and each kind's createAttributes
func (m *Mob) AttributeSupplier() *player.Supplier {
	if m.zombie.LeaderHealth != 0 || IsEquine(m.Type) {
		return m.buildSupplier() // per-mob bases (leader health, rolled horse stats): not shareable
	}
	speed := m.paceBase()
	key := supplierKey{m.Type, m.Size, speed, m.MaxHealth, m.KillerBunny()}
	if s, ok := supplierCache.Load(key); ok {
		return s.(*player.Supplier)
	}
	s, _ := supplierCache.LoadOrStore(key, m.buildSupplier())
	return s.(*player.Supplier)
}

func (m *Mob) buildSupplier() *player.Supplier {
	s := &player.Supplier{Names: map[string]bool{"minecraft:follow_range": true}, Bases: map[string]float64{}}
	for _, name := range livingAttributes {
		s.Names["minecraft:"+name] = true
	}
	k := m.kind()
	family := m.family()
	speed := m.paceBase()
	// vanilla: Mob.createMobAttributes; the zombie, happy ghast and every
	// hostile kind override it through BaseFollowRange.
	s.Bases["minecraft:follow_range"] = m.BaseFollowRange()
	if m.Size > 0 && m.Type != TypeSulfurCube || Enemy(m.Type) || k.attackDamage > 0 {
		s.Names["minecraft:attack_damage"] = true
		// vanilla: Monster.createMonsterAttributes leaves the base at 2.
		s.Bases["minecraft:attack_damage"] = k.attackDamage
	}
	if m.Type == TypeSulfurCube {
		s.Names["minecraft:tempt_range"] = true
		s.Bases["minecraft:tempt_range"] = 8
	}
	if mobIsAnimal(family) || IsEquine(m.Type) || m.Type == TypeTadpole {
		// vanilla: Animal.createAnimalAttributes: 10, the default; the
		// equines and the tadpole build on it without being an Animal here.
		s.Names["minecraft:tempt_range"] = true
	}
	if family == TypeHappyGhast {
		s.Names["minecraft:flying_speed"] = true
		s.Bases["minecraft:tempt_range"] = 16
		s.Bases["minecraft:flying_speed"] = 0.05
		s.Bases["minecraft:camera_distance"] = 8
	}
	if fs := flyingSpeedBase(family); fs != 0 {
		s.Names["minecraft:flying_speed"] = true
		s.Bases["minecraft:flying_speed"] = fs
	}
	if family == TypeGhast {
		// vanilla: Ghast.createAttributes; movement_speed keeps the registry
		// default of 0.7.
		s.Names["minecraft:flying_speed"] = true
		s.Bases["minecraft:flying_speed"] = 0.06
		s.Bases["minecraft:camera_distance"] = 8
	}
	if family == TypeZombie {
		s.Names["minecraft:spawn_reinforcements"] = true
	}
	if k.stepHeight != 0 {
		s.Bases["minecraft:step_height"] = k.stepHeight
	}
	if a := m.kindArmor(); a != 0 {
		s.Bases["minecraft:armor"] = a
	}
	if k.attackKnockback != 0 {
		s.Bases["minecraft:attack_knockback"] = k.attackKnockback
	}
	if kr := k.knockbackResist; kr != 0 {
		s.Bases["minecraft:knockback_resistance"] = kr
	}
	s.Bases["minecraft:safe_fall_distance"], s.Bases["minecraft:fall_damage_multiplier"] = m.fallAttributeBases()
	s.Bases["minecraft:movement_speed"] = speed
	if family == TypeHappyGhast {
		s.Bases["minecraft:movement_speed"] = 0.05
	}
	if family == TypeGhast {
		s.Bases["minecraft:movement_speed"] = 0.7
	}
	s.Bases["minecraft:max_health"] = m.MaxHealth
	if lh := m.zombie.LeaderHealth; lh != 0 {
		s.Bases["minecraft:max_health"] = m.MaxHealth / (1 + lh)
	}
	if m.Size > 0 {
		// vanilla: AbstractCubeMob.setSize, Slime.setSize, MagmaCube.setSize
		s.Bases["minecraft:movement_speed"] = float64(float32(0.2) + float32(0.1)*float32(m.Size))
		if m.Type != TypeSulfurCube {
			s.Bases["minecraft:attack_damage"] = float64(m.Size)
		}
		if m.Type == TypeMagmaCube {
			s.Bases["minecraft:armor"] = float64(3 * m.Size)
		}
	}
	if IsEquine(m.Type) {
		// vanilla: AbstractHorse.createBaseHorseAttributes, rolled stats
		s.Bases["minecraft:movement_speed"] = m.Horse.Speed
		s.Bases["minecraft:jump_strength"] = m.Horse.Jump
	}
	return s
}

// fallAttributeBases are the kind's unmodified fall attributes.
// vanilla: AbstractHorse.createBaseHorseAttributes, Fox.createAttributes
func (m *Mob) fallAttributeBases() (safe, multiplier float64) {
	switch {
	case IsEquine(m.Type):
		return 6, 0.5
	case m.Type == TypeFox:
		return 5, 1
	default:
		return 3, 1
	}
}

// flyingSpeedBase is the kind's flying_speed base, 0 for a kind without the
// attribute; the happy ghast and ghast set theirs in buildSupplier.
// vanilla: Bee, Parrot, Allay and WitherBoss.createAttributes
func flyingSpeedBase(t MobType) float64 {
	switch t {
	case TypeBee:
		return beeFlyingSpeed
	case TypeParrot:
		return parrotFlyingSpeed
	case TypeAllay:
		return float64(float32(0.1))
	case TypeWither:
		return float64(float32(0.6))
	}
	return 0
}

// mobIsAnimal reports the Animal kinds whose attributes start from
// Animal.createAnimalAttributes, less the equines, which dismount their own
// way (AbstractHorse.getDismountLocationForPassenger).
func mobIsAnimal(t MobType) bool {
	switch t {
	case TypeCow, TypeMooshroom, TypePig, TypeSheep, TypeChicken, TypeTurtle, TypeHappyGhast, TypeRabbit,
		TypeArmadillo, TypeSniffer, TypeCat, TypeOcelot, TypePanda, TypePolarBear, TypeGoat, TypeFox, TypeStrider, TypeAxolotl, TypeNautilus, TypeZombieNautilus, TypeParrot,
		TypeBee, TypeWolf, TypeFrog:
		return true
	}
	return false
}

// kindArmor is the armor attribute's base: the kind's, 8 for the killer
// bunny.
// vanilla: Rabbit.setVariant
func (m *Mob) kindArmor() float64 {
	if m.KillerBunny() {
		return rabbitEvilArmor
	}
	return m.kind().armor
}

// baseArmor is the armor without equipment: kindArmor plus a closed
// shulker's 20.
// vanilla: Shulker.setRawPeekAmount COVERED_ARMOR_MODIFIER
func (m *Mob) baseArmor() float64 {
	if m.IsShulker() && m.shulker.covered {
		return m.kindArmor() + shulkerCoveredArmor
	}
	return m.kindArmor()
}

const modCovered = "minecraft:covered"

// equipmentSlot pairs a stack with the slot its modifiers apply in.
type equipmentSlot struct {
	stack protocol.ItemStack
	name  string
}

func livingEquipmentSlots(worn [4]protocol.ItemStack, mainHand, offHand protocol.ItemStack) [7]equipmentSlot {
	var out [7]equipmentSlot
	for i, name := range ArmorSlotNames {
		out[i] = equipmentSlot{worn[i], name}
	}
	out[4], out[5] = equipmentSlot{mainHand, "mainhand"}, equipmentSlot{offHand, "offhand"}
	return out
}

func (m *Mob) equipmentSlots() [7]equipmentSlot {
	out := livingEquipmentSlots(m.Worn, m.MainHand, m.OffHand)
	out[6] = equipmentSlot{m.Body, "body"}
	return out
}

// SyncEquipmentModifiers moves the modifiers of each slot whose stack changed
// since the last call onto the map: the old stack's come off, the new one's
// go on. A closed shulker's armor rides along.
// vanilla: LivingEntity.collectEquipmentChanges, Shulker.setRawPeekAmount
func (m *Mob) SyncEquipmentModifiers(a *player.Attributes) {
	if !m.attrWornValid || m.attrWornRead != m.EquipmentRevision() {
		syncEquipmentModifiers(a, m.equipmentSlots(), &m.attrWorn)
		m.attrWornRead, m.attrWornValid = m.EquipmentRevision(), true
	}
	if m.IsShulker() {
		m.syncShulkerArmor(a)
	}
}

// syncEquipmentModifiers removes all changed slots before applying replacements,
// so a modifier shared across slots survives an equipment swap.
// vanilla: LivingEntity.collectEquipmentChanges
func syncEquipmentModifiers(a *player.Attributes, slots [7]equipmentSlot, previous *[7]protocol.ItemStack) {
	var changed [7]bool
	for i, sl := range slots {
		if sl.stack.Equal(previous[i]) {
			continue
		}
		changed[i] = true
		for _, mod := range rules.EquipmentModifiers(previous[i], sl.name) {
			a.RemoveModifier(mod.Attribute, mod.ID)
		}
	}
	for i, sl := range slots {
		if !changed[i] {
			continue
		}
		applyEquipment(a, sl.stack, sl.name, nil)
		previous[i] = sl.stack
	}
}

// applyEquipment adds the stack's slot modifiers for attributes the map has;
// when fresh is non-nil it collects the IDs this call instantiated.
func applyEquipment(a *player.Attributes, stack protocol.ItemStack, slot string, fresh *[]registry.AttributeID) {
	for _, mod := range rules.EquipmentModifiers(stack, slot) {
		if !a.Has(mod.Attribute) {
			continue // vanilla: AttributeMap.getInstance is null
		}
		if fresh != nil && !a.Instantiated(mod.Attribute) {
			*fresh = append(*fresh, mod.Attribute)
		}
		a.AddModifier(mod.Attribute, player.Modifier{ID: mod.ID, Amount: mod.Amount, Operation: mod.Operation})
	}
}

// paceBase is the movement_speed base the body runs at: the cube's size
// pace, a horse's roll, the ravager's walk, the kind's own otherwise. It is
// the value beside movement_speed that pace keeps its quirks off of.
func (m *Mob) paceBase() float64 {
	switch {
	case m.isCube():
		return m.cubePace()
	case IsEquine(m.Type) && m.Horse.Speed > 0:
		return m.Horse.Speed
	case m.Type == TypeRavager:
		return ravagerWalkPace
	case m.Type == TypePanda && m.pandaVariant() == GeneLazy:
		return float64(float32(0.07)) // vanilla: Panda.setAttributes
	}
	return m.kind().speed
}

// attrPaced reports whether the body's pace comes from its movement_speed
// attribute: every kind's does but the ghast's, which flies by its own
// model while the attribute keeps the 0.7 a pairing reports.
// vanilla: Ghast.travel
func (m *Mob) attrPaced() bool { return m.family() != TypeGhast }

// BuildAttributes makes the mob's attribute map: the kind's supplier, the
// instances a pairing carries, and whatever the saved list folds in. It does
// not install the map on the body, and the dirty the list's reads mark
// stays pending: a load lets the next tick announce it.
func (m *Mob) BuildAttributes(saved nbt.List) *player.Attributes {
	a := player.NewAttributes(m.AttributeSupplier())
	a.Instance(registry.AttributeMovementSpeed)
	// Vanilla instantiates movement_speed and the equipment's attributes in
	// the first tick, not with the map.
	synthetic := []registry.AttributeID{registry.AttributeMovementSpeed}
	if m.Size > 0 {
		a.Instance(registry.AttributeMaxHealth)
		if m.Type == TypeMagmaCube {
			a.Instance(registry.AttributeArmor)
		}
	}
	if m.NativeAttributeInstances() {
		a.Instance(registry.AttributeAttackDamage)
	}
	if m.KillerBunny() {
		// vanilla: Rabbit.setVariant sets the armor base and adds a transient attack modifier
		a.Instance(registry.AttributeArmor)
		a.AddModifier(registry.AttributeAttackDamage, player.Modifier{ID: "minecraft:evil",
			Amount: rabbitEvilAttack, Operation: registry.AttributeAddValue})
	}
	if IsEquine(m.Type) {
		if m.Horse.healthSet {
			a.Instance(registry.AttributeMaxHealth)
		}
		if m.Horse.jumpSet {
			a.Instance(registry.AttributeJumpStrength)
		}
	}
	if lh := m.zombie.LeaderHealth; lh != 0 {
		// vanilla: Zombie.ZOMBIE_LEADER_BONUS; the supplier's base holds
		// the pre-bonus maximum, and the modifier folds it back so an
		// install cannot trim a healthy leader down to the base.
		a.AddModifier(registry.AttributeMaxHealth, player.Modifier{ID: modLeaderBonus, Amount: lh,
			Operation: registry.AttributeAddMultipliedTotal, Persistent: true})
	}
	m.syncFollowSpawnBonus(a)
	m.syncZombieTraits(a)
	for _, sl := range m.equipmentSlots() {
		applyEquipment(a, sl.stack, sl.name, &synthetic)
	}
	// The mark must follow every Instance above; Load and the sync below
	// instantiate for real and clear it.
	for _, id := range synthetic {
		a.MarkSynthetic(id)
	}
	a.Load(saved)
	// Shell transitions supersede the native modifier kept by the last load.
	if m.IsShulker() {
		m.syncShulkerArmor(a)
	}
	m.SyncSpeedModifiers(a)
	return a
}

// NativeAttributeInstances reports whether the kind instantiates attributes
// itself before any command touches the map.
// vanilla: Rabbit.setVariant gets attack_damage on load and spawn
func (m *Mob) NativeAttributeInstances() bool { return m.Type == TypeRabbit }

// Ticked reports whether the body has run a tick.
func (m *Mob) Ticked() bool { return m.ticked }

// SetAttributes installs the attribute map the body reads its pace and
// health cap in through; nil for none.
func (m *Mob) SetAttributes(a *player.Attributes) {
	m.attrs = a
	m.attrWorn = [7]protocol.ItemStack{} // the next sync lays every slot's modifiers again
	m.attrWornValid = false
}

// Attributes is the installed attribute map, nil for none.
func (m *Mob) Attributes() *player.Attributes { return m.attrs }

// attrOr is the attribute's value when the body carries a map holding it
// (with its modifiers), else direct, the model's own value.
func (m *Mob) attrOr(attr registry.AttributeID, direct float64) float64 {
	if m.attrs != nil && m.attrs.Has(attr) {
		return m.attrs.Value(attr)
	}
	return direct
}

// The mob's own movement_speed modifiers; the baby's and the witch's are
// permanent in vanilla, the frost slow, enderman anger and strider shiver transient.
// vanilla: Zombie.SPEED_MODIFIER_BABY, Witch.SPEED_MODIFIER_DRINKING,
// EnderMan.SPEED_MODIFIER_ATTACKING_ID,
// LivingEntity.SPEED_MODIFIER_POWDER_SNOW_ID, Strider.SUFFOCATING_MODIFIER_ID
const (
	modBaby        = "minecraft:baby"
	modDrink       = "minecraft:drinking"
	modFrost       = "minecraft:powder_snow"
	modAttacking   = "minecraft:attacking"
	modSuffocating = "minecraft:suffocating"
)

// SyncSpeedModifiers updates the body's movement modifiers and Jump Boost's
// safe fall distance so the simulation and attribute packets agree.
// vanilla: LivingEntity.tryAddFrost, Witch.aiStep, Zombie.tick,
// EnderMan.setTarget, Strider.setSuffocating, MobEffects.SPEED, SLOWNESS, JUMP_BOOST
func (m *Mob) SyncSpeedModifiers(a *player.Attributes) {
	if m.Type == TypeSulfurCube {
		m.syncSulfurModifiers(a)
	}
	if m.TicksFrozen > 0 && !m.underfootAir {
		a.AddModifier(registry.AttributeMovementSpeed, player.Modifier{ID: modFrost,
			Amount: rules.FreezeSpeedAmount(m.TicksFrozen), Operation: registry.AttributeAddValue})
	} else {
		a.RemoveModifier(registry.AttributeMovementSpeed, modFrost)
	}
	rules.UpdateEffectAttributesOf(a, m.effects.All(), registry.AttributeMovementSpeed)
	rules.UpdateEffectAttributesOf(a, m.effects.All(), registry.AttributeSafeFallDistance)
	switch {
	case m.IsStrider():
		if m.StriderCold() {
			a.AddModifier(registry.AttributeMovementSpeed, player.Modifier{ID: modSuffocating, Amount: striderColdSpeed, Operation: registry.AttributeAddMultipliedBase})
		} else {
			a.RemoveModifier(registry.AttributeMovementSpeed, modSuffocating)
		}
	case m.Type == TypeWitch:
		// vanilla: Witch.SPEED_MODIFIER_DRINKING
		if m.DrinkingPotion() {
			a.AddModifier(registry.AttributeMovementSpeed, player.Modifier{ID: modDrink, Amount: witchDrinkSpeed,
				Operation: registry.AttributeAddValue})
		} else {
			a.RemoveModifier(registry.AttributeMovementSpeed, modDrink)
		}
	case m.Type == TypeEnderman:
		// vanilla: EnderMan.SPEED_MODIFIER_ATTACKING
		if m.Creepy() {
			a.AddModifier(registry.AttributeMovementSpeed, player.Modifier{ID: modAttacking,
				Amount: float64(float32(0.15)), Operation: registry.AttributeAddValue})
		} else {
			a.RemoveModifier(registry.AttributeMovementSpeed, modAttacking)
		}
	case m.is(TypeZombie):
		if m.IsBaby() && !m.isCube() {
			a.AddModifier(registry.AttributeMovementSpeed, player.Modifier{ID: modBaby, Amount: 0.5,
				Operation: registry.AttributeAddMultipliedBase})
		} else {
			a.RemoveModifier(registry.AttributeMovementSpeed, modBaby)
		}
	}
}

// healthCap is the body's most health: the attribute's value with a map
// installed, the model's otherwise. Unedited the two agree — the supplier's
// bases compose to the model's maximum.
// vanilla: LivingEntity.getMaxHealth
func (m *Mob) healthCap() float64 {
	if m.attrs != nil {
		return m.attrs.Value(registry.AttributeMaxHealth)
	}
	return m.MaxHealth
}

// syncMaxHealth writes the model's maximum through an installed map, as the
// pre-bonus base and the leader modifier, so the cap follows a model change.
func (m *Mob) syncMaxHealth() {
	if m.attrs == nil {
		return
	}
	lh := m.zombie.LeaderHealth
	m.attrs.SetBase(registry.AttributeMaxHealth, m.MaxHealth/(1+lh))
	if lh != 0 {
		m.attrs.AddModifier(registry.AttributeMaxHealth, player.Modifier{ID: modLeaderBonus, Amount: lh,
			Operation: registry.AttributeAddMultipliedTotal, Persistent: true})
	} else {
		m.attrs.RemoveModifier(registry.AttributeMaxHealth, modLeaderBonus)
	}
}

// attrScale is the scale attribute read as a float and held to the kind's
// limit, 1 for a body without a map: only a command's edit installs one.
// vanilla: LivingEntity.getScale
func (m *Mob) attrScale() float64 {
	if m.attrs == nil {
		return 1
	}
	s := float64(float32(m.attrs.Value(registry.AttributeScale)))
	// vanilla: EnderDragon/HappyGhast/Shulker.sanitizeScale
	switch m.Type {
	case TypeEnderDragon:
		return 1
	case TypeHappyGhast:
		return min(s, 1)
	case TypeShulker:
		return min(s, 3)
	}
	return s
}

// OnAttributeUpdated applies an attribute change's limits to the body:
// health above the new maximum trims down to it. Callers hold the mob lock.
// A scale change reaches the box at its next read, deliberately without
// vanilla's fudgePositionAfterSizeChange.
// vanilla: LivingEntity.onAttributeUpdated
func (m *Mob) OnAttributeUpdated() {
	if m.Health > m.healthCap() {
		m.SetMetadataHealth(m.healthCap())
	}
}

// savedAttrEdits reports whether the saved attributes list holds an
// attribute the kind's own save (the zombie's traits, an equine's rolls) does
// not write: a command's edit. Such a list belongs to the map, so a load
// gives the body its map; a list the model owns wakes nothing. It runs after
// the reads that set m's bases.
func savedAttrEdits(list nbt.List, m *Mob) bool {
	t := m.Type
	for _, item := range list.Items {
		c, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		id, _ := nbt.Get[nbt.String](c, "id")
		name := registry.Qualify(string(id))
		if t == TypeShulker && name == attrArmor && onlyShulkerArmor(c) {
			continue
		}
		if t == TypeWolf && name == attrMaxHealth {
			if wolfTamingBase(m, c) {
				continue
			}
			return true
		}
		// A zombie's health traits model only its leader modifier and the kind's base.
		if Family(t) == TypeZombie && name == attrMaxHealth {
			base, ok := nbt.NumberValue(c["base"])
			if ok && base != m.MaxHealth/(1+m.zombie.LeaderHealth) {
				return true
			}
			mods, _ := nbt.Get[nbt.List](c, "modifiers")
			for _, item := range mods.Items {
				mod, _ := nbt.CompoundView(item)
				id, _ := nbt.Get[nbt.String](mod, "id")
				op, _ := nbt.Get[nbt.String](mod, "operation")
				if registry.Qualify(string(id)) != modLeaderBonus || string(op) != opAddMultiplyTot {
					return true
				}
			}
		}
		if IsEquine(t) && horseAttrs[name] {
			mods, _ := nbt.Get[nbt.List](c, "modifiers")
			// Rolled stats are positive; keep command bases outside that range in the map.
			if nbt.DoubleFieldOr(c, "base", 0) <= 0 || len(mods.Items) != 0 {
				return true
			}
			continue
		}
		if traitAttributes(t)[name] ||
			name == attrFollowRange && onlySpawnBonus(c, m.BaseFollowRange()) {
			continue
		}
		return true
	}
	return false
}

// horseAttrs are the rolled attributes an equine's own save writes.
var horseAttrs = map[string]bool{
	attrMaxHealth: true, "minecraft:movement_speed": true, "minecraft:jump_strength": true,
}
