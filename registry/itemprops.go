package registry

// The per-item default component data (stack sizes, durability, tools, food,
// combat, armour), generated into itemprop_gen.go by tools/gen itemprops
// in item registry order. Lookups take the numeric item ID packets carry.

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// ItemProps holds what an item's default data components decide; an absent
// field is zero.
type ItemProps struct {
	// MaxStack is the item's minecraft:max_stack_size: how many of it one
	// slot holds, and how many of it a merge stops at.
	MaxStack int32
	// MaxDamage is the item's minecraft:max_damage: the wear it takes
	// before it breaks. Zero for an item that never wears.
	MaxDamage int32
	// Tool is the item's minecraft:tool, nil for one that is no tool.
	Tool *Tool
	// BlockTransformer is the built-in conversion used on a block, empty for none.
	BlockTransformer BlockTransformer
	// Food is the item's minecraft:food, nil for one that is not a meal.
	Food *Food
	// Consumable is minecraft:consumable where it says more than Food; nil
	// for a plain meal. Potion, suspicious stew and ominous bottle effects
	// come from the stack's own components when the use finishes.
	Consumable *Consumable
	// Weapon is the item's minecraft:weapon, nil for a non-weapon.
	Weapon *Weapon
	// Modifiers is minecraft:attribute_modifiers in component order.
	Modifiers []AttributeModifier
	// Enchantable is minecraft:enchantable's enchantability; 0 means no
	// table takes it.
	Enchantable int32
	// Repairable is minecraft:repairable's raw reference: "#tag" or an item
	// name; empty when nothing repairs it.
	Repairable string
	// BlocksAttacks is the item's minecraft:blocks_attacks, the component
	// that makes it a shield; nil for an item that blocks nothing.
	BlocksAttacks *BlocksAttacks
	// NoInteractVibrations is minecraft:use_effects' interact_vibrations
	// turned off: the item's uses post no item_interact game events.
	NoInteractVibrations bool
	// Glider is the item's minecraft:glider: worn in its equippable slot, it
	// lets the wearer fall-fly.
	Glider bool
	// DeathProtection is minecraft:death_protection's consume effects; nil
	// for an item that saves nobody.
	DeathProtection []ConsumeEffect
	// AttackRange is the item's minecraft:attack_range, nil for an item
	// that attacks at the holder's own entity interaction range.
	AttackRange *AttackRange
	// PiercingWeapon is the item's minecraft:piercing_weapon, the jab a
	// spear's Stab player action runs; nil for an item that has none.
	PiercingWeapon *PiercingWeapon
	// KineticWeapon is the item's minecraft:kinetic_weapon, the charge a
	// spear's use runs; nil for an item that has none.
	KineticWeapon *KineticWeapon
	// AttackAnimation is the item's minecraft:attack_animation when it is not
	// the default; nil for an item that swings with SwingAnimation's default.
	AttackAnimation *SwingAnimation
	// MinimumAttackCharge is the item's minecraft:minimum_attack_charge,
	// the attack strength scale (0 to 1) below which it cannot attack.
	MinimumAttackCharge float32
	// DamageType is the item's minecraft:damage_type, the damage type its
	// blows deal; empty for the attacker's own (player_attack).
	DamageType DamageType
}

// AttackRange is minecraft:attack_range: the reaches in blocks, the hitbox
// margin, and the factor a mob holding it scales survival reaches by.
type AttackRange struct {
	MinReach, MaxReach                 float32
	MinCreativeReach, MaxCreativeReach float32
	HitboxMargin                       float32
	MobFactor                          float32
}

// PiercingWeapon is minecraft:piercing_weapon: whether a jab knocks back and
// dismounts, and its sounds (SoundNone for none).
type PiercingWeapon struct {
	DealsKnockback, Dismounts bool
	Sound, HitSound           Sound
}

// SwingAnimation is minecraft:attack_animation: the swing type on the wire
// (0 none, 1 whack, 2 stab) and its length in ticks.
// vanilla: SwingAnimation, SwingAnimationType
type SwingAnimation struct {
	Type, Duration int32
}

// DefaultSwingAnimation is the whack of six ticks an item without its own
// swings with.
// vanilla: SwingAnimation.DEFAULT
var DefaultSwingAnimation = SwingAnimation{Type: 1, Duration: 6}

// AttackAnimationOf returns the item's attack animation, the default for an
// item that does not set one.
func AttackAnimationOf(item int32) SwingAnimation {
	if a := propsOf(item).AttackAnimation; a != nil {
		return *a
	}
	return DefaultSwingAnimation
}

// KineticWeapon is minecraft:kinetic_weapon: the use of a spear whose speed
// decides what a contact does. A nil condition never holds; forward movement
// is not kept.
type KineticWeapon struct {
	// ContactCooldown is the ticks before the same body is hit again, Delay
	// the ticks of use before the first hit.
	ContactCooldown, Delay      int32
	Dismount, Knockback, Damage *KineticCondition
	DamageMultiplier            float32
	// Sound is played when a player starts the charge, HitSound is the
	// client's own feedback for a contact (entity event hurt); SoundNone for none.
	Sound, HitSound Sound
}

// KineticCondition is KineticWeapon.Condition.
type KineticCondition struct {
	MaxDuration                int32
	MinSpeed, MinRelativeSpeed float32
}

// Test is Condition.test: speeds in blocks per second, entityFactor scaling
// the minimums for a mob.
func (c *KineticCondition) Test(ticksUsed int32, speed, relative, entityFactor float64) bool {
	return c != nil && ticksUsed <= c.MaxDuration && speed >= float64(c.MinSpeed)*entityFactor &&
		relative >= float64(c.MinRelativeSpeed)*entityFactor
}

// ComputeDamageUseDuration is KineticWeapon.computeDamageUseDuration.
func (k *KineticWeapon) ComputeDamageUseDuration() int32 {
	n := k.Delay
	if k.Damage != nil {
		n += k.Damage.MaxDuration
	}
	return n
}

// Tool is minecraft:tool: mining speed and correct-tool rules, and the wear
// breaking a block costs.
type Tool struct {
	Rules []ToolRule
	// DefaultMiningSpeed is the speed for a block no rule gives one for.
	DefaultMiningSpeed float32
	// DamagePerBlock is the wear per block broken; 1 when the component
	// leaves it out.
	DamagePerBlock int32
	// CanDestroyBlocksInCreative is false for swords, the trident and the
	// mace, and true (the default) where the report leaves it out.
	CanDestroyBlocksInCreative bool
}

// ToolBlocks is the blocks a tool rule covers: the members of its tags as
// loaded (data packs may change them) and its inline blocks.
type ToolBlocks struct {
	Tags   []string
	Blocks []Block
}

// Contains reports whether the block is inline or in one of the tags.
// vanilla: BlockState.is(HolderSet)
func (tb *ToolBlocks) Contains(b Block) bool {
	if slices.Contains(tb.Blocks, b) {
		return true
	}
	for _, tag := range tb.Tags {
		if b.InTag(BlockTagOf(tag)) {
			return true
		}
	}
	return false
}

// ToolRule is one tool rule: its blocks and what it says; a half left out
// falls to the next covering rule.
type ToolRule struct {
	Blocks *ToolBlocks
	// Speed is the rule's mining speed, zero when it carries none.
	Speed float32
	// CorrectForDrops and DeniesDrops are the rule's correct_for_drops
	// true and false; neither is set when the rule leaves it out.
	CorrectForDrops bool
	DeniesDrops     bool
}

// Food is the eating side of an item: what a meal of it restores and how
// long the meal takes.
type Food struct {
	// Nutrition is the food bar-filling of one meal, in shank icons - the
	// half-shanks the Set Health packet carries its food level as.
	Nutrition int32
	// Saturation is the saturation one meal adds; the component carries the
	// gain itself, not the old modifier, capped by the food bar after it.
	Saturation float32
	// CanAlwaysEat is the component's can_always_eat: a meal that can be
	// begun on a full food bar, the golden apples being the ones that can.
	CanAlwaysEat bool
	// ConsumeSeconds is how long the use is held, 1.6 for 32 ticks of
	// eating.
	ConsumeSeconds float32
}

// Consumable is minecraft:consumable as a finished use reads it, with
// minecraft:use_remainder; Drink sounds the bites differently.
type Consumable struct {
	Seconds   float32
	Effects   []ConsumeEffect
	Remainder protocol.ItemStack
	Sound     protocol.SoundHolder
	Drink     bool
}

// defaultConsumeSound is the consumable component's default sound.
const defaultConsumeSound = "minecraft:entity.generic.eat"

// ConsumeEffectType is the type of an on_consume_effects entry.
type ConsumeEffectType uint8

// The consume effect types (vanilla: ConsumeEffect.Type).
const (
	ApplyEffects ConsumeEffectType = iota
	RemoveEffects
	ClearAllEffects
	TeleportRandomly
	PlaySound
)

// ConsumeEffect is one on_consume_effects entry; the fields its type does
// not use are zero.
type ConsumeEffect struct {
	Type        ConsumeEffectType
	Effects     []EffectSpec         // apply_effects
	Probability float32              // apply_effects
	Remove      []string             // remove_effects
	Diameter    float32              // teleport_randomly
	Directional bool                 // teleport_randomly: the consume-teleport level event is sent
	Sound       protocol.SoundHolder // play_sound
}

// EffectSpec is a status effect as a component writes it (vanilla's
// MobEffectInstance codec), by effect name.
type EffectSpec struct {
	Effect                           string
	Amplifier, Duration              int32
	Ambient, ShowParticles, ShowIcon bool
}

// ConsumableOf returns how the item is consumed, if it can be. A plain
// meal's is its food's consume time and nothing more.
func ConsumableOf(item int32) (Consumable, bool) {
	props := propsOf(item)
	var c Consumable
	switch {
	case props.Consumable != nil:
		c = *props.Consumable
	case props.Food != nil:
		c = Consumable{Seconds: props.Food.ConsumeSeconds, Sound: SoundEntityGenericEat.Holder()}
	default:
		return Consumable{}, false
	}
	return c, true
}

// itemProps indexes the generated table by numeric item ID once, since most
// questions are asked per stack.
var itemProps = func() []ItemProps {
	props := make([]ItemProps, len(Items))
	for id, name := range Items {
		props[id] = ItemProperties[name]
	}
	return props
}()

// propsOf is the components of one item, or the nothing-that-applies of an
// item this version does not name.
func propsOf(item int32) ItemProps {
	if item < 0 || int(item) >= len(itemProps) {
		return ItemProps{}
	}
	return itemProps[item]
}

// MaxStackSize returns an item's stack size; 64 for an item without
// components here, the default not worth sending.
func MaxStackSize(item int32) int32 {
	if props := propsOf(item); props.MaxStack > 0 {
		return props.MaxStack
	}
	return 64
}

// MaxDamage returns the wear an item takes before it breaks, 0 for one that
// never wears.
func MaxDamage(item int32) int32 {
	return propsOf(item).MaxDamage
}

// ItemHasComponentDefault reports whether the item prototype carries the named component.
// vanilla: Item.components
func ItemHasComponentDefault(item int32, name string) bool {
	id, ok := ComponentID(name)
	return ok && item >= 0 && int(item) < len(itemComponentDefaults) &&
		itemComponentDefaults[item][id/64]&(uint64(1)<<(id%64)) != 0
}

// Weapon is minecraft:weapon: the wear a landed hit costs and the seconds it
// disables a blocking shield for.
// vanilla: Weapon.CODEC
type Weapon struct {
	DamagePerAttack        int32
	DisableBlockingSeconds float32
}

// BlocksAttacks is minecraft:blocks_attacks. A nil BypassedBy bypasses
// nothing and a nil sound plays nothing.
// vanilla: BlocksAttacks.CODEC
type BlocksAttacks struct {
	BlockDelaySeconds    float32
	DisableCooldownScale float32
	Reductions           []DamageReduction
	ItemDamage           ItemDamageFunction
	BypassedBy           *DamageTypeSet
	BlockSound           *protocol.SoundHolder
	DisableSound         *protocol.SoundHolder
}

// DamageReduction is one entry of damage_reductions; a nil Type matches
// every damage type.
// vanilla: BlocksAttacks.DamageReduction
type DamageReduction struct {
	Angle  float32 // horizontal_blocking_angle, degrees
	Type   *DamageTypeSet
	Base   float32
	Factor float32
}

// ItemDamageFunction is the wear a blocked amount costs the item.
// vanilla: BlocksAttacks.ItemDamageFunction
type ItemDamageFunction struct {
	Threshold, Base, Factor float32
}

// DamageTypeSet is a bound damage type holder set: a tag or explicit IDs.
// vanilla: HolderSet.Named, HolderSet.Direct
type DamageTypeSet struct {
	Tag   DamageTypeTagID
	IsTag bool
	Types []DamageType
}

// Contains reports whether the damage type is in the set.
func (s *DamageTypeSet) Contains(damageType DamageType) bool {
	if s.IsTag {
		return damageType.InTag(s.Tag)
	}
	return slices.Contains(s.Types, damageType)
}

// ItemBlocksAttacks returns the item's own minecraft:blocks_attacks, nil for
// an item that blocks nothing.
func ItemBlocksAttacks(item int32) *BlocksAttacks {
	return propsOf(item).BlocksAttacks
}

// DeathProtectionOf returns minecraft:death_protection's effects; false for
// an item that saves nobody.
func DeathProtectionOf(item int32) ([]ConsumeEffect, bool) {
	effects := propsOf(item).DeathProtection
	return effects, effects != nil
}

// Enchantable returns minecraft:enchantable's value; 0 makes a table offer
// nothing.
func Enchantable(item int32) int32 {
	return propsOf(item).Enchantable
}

// CanDestroyBlocksInCreative reports the tool component's answer (true for a
// non-tool); the item's own canDestroyBlock may still refuse, as the debug
// stick does in every mode.
// vanilla: Item.canDestroyBlock
func CanDestroyBlocksInCreative(item int32) bool {
	tool := propsOf(item).Tool
	return tool == nil || tool.CanDestroyBlocksInCreative
}

// DamagePerBlock returns the wear breaking one block adds to the item when
// it is held, zero for an item that is no tool and takes none.
func DamagePerBlock(item int32) int32 {
	if tool := propsOf(item).Tool; tool != nil {
		return tool.DamagePerBlock
	}
	return 0
}

// HasTool reports whether the item carries a tool component.
func HasTool(item int32) bool {
	return propsOf(item).Tool != nil
}

// ItemWeapon returns the item's own minecraft:weapon, nil for a non-weapon.
func ItemWeapon(item int32) *Weapon {
	return propsOf(item).Weapon
}

// AttackRangeOf returns the item's attack_range component, false for an
// item without one.
func AttackRangeOf(item int32) (AttackRange, bool) {
	if r := propsOf(item).AttackRange; r != nil {
		return *r, true
	}
	return AttackRange{}, false
}

// PiercingWeaponOf returns the item's piercing_weapon component, false for
// an item without one.
func PiercingWeaponOf(item int32) (PiercingWeapon, bool) {
	if p := propsOf(item).PiercingWeapon; p != nil {
		return *p, true
	}
	return PiercingWeapon{}, false
}

// InteractVibrations reports whether the item's uses post the
// item_interact game events. vanilla: ItemStack.causeUseVibration
func InteractVibrations(item int32) bool { return !propsOf(item).NoInteractVibrations }

// KineticWeaponOf returns the item's kinetic_weapon component, nil for an
// item without one.
func KineticWeaponOf(item int32) *KineticWeapon { return propsOf(item).KineticWeapon }

// MinimumAttackCharge returns the item's minimum_attack_charge, zero for an
// item that attacks at any charge.
func MinimumAttackCharge(item int32) float32 {
	return propsOf(item).MinimumAttackCharge
}

// ItemDamageType returns the damage type the item's blows deal, "" for an
// item without a damage_type component.
func ItemDamageType(item int32) DamageType {
	return propsOf(item).DamageType
}

// ItemModifiers returns an item's default attribute modifiers in component
// order. The slice is shared; callers must not change it.
func ItemModifiers(item int32) []AttributeModifier {
	return propsOf(item).Modifiers
}

// StackModifiers is the stack's minecraft:attribute_modifiers: its own
// component where it carries one, even an empty one, and the item's default
// otherwise.
// vanilla: ItemStack.forEachModifier, getOrDefault(ATTRIBUTE_MODIFIERS)
func StackModifiers(stack protocol.ItemStack) []AttributeModifier {
	if stack.Removes(protocol.DataComponentAttributeModifiers) {
		return nil
	}
	entries, ok := stack.AttributeModifiersEffective()
	if !ok {
		return ItemModifiers(stack.ItemID)
	}
	out := make([]AttributeModifier, 0, len(entries))
	for _, e := range entries {
		if e.Attribute < 0 || e.Attribute >= NumAttributes {
			continue
		}
		out = append(out, AttributeModifier{
			Attribute: AttributeID(e.Attribute),
			ID:        e.ID,
			Amount:    e.Amount,
			Operation: AttributeOperation(e.Operation),
			Slot:      slotGroupNames[e.Slot],
		})
	}
	return out
}

// AttackDamage returns the main-hand attack modifier; the fist's base is the
// caller's to add.
func AttackDamage(item int32) float32 {
	return float32(addedTo(item, AttributeAttackDamage, "mainhand"))
}

// ArmorOf returns what the item contributes to the armour of whoever wears
// it: its points, and the toughness that turns the heaviest blows off them.
func ArmorOf(item int32) (points, toughness float32) {
	return float32(addedTo(item, AttributeArmor, "")),
		float32(addedTo(item, AttributeArmorToughness, ""))
}

// addedTo sums the item's add_value modifiers of one attribute, those whose
// group holds the slot, or every one of them for an empty slot.
func addedTo(item int32, attribute AttributeID, slot string) float64 {
	var sum float64
	for _, m := range propsOf(item).Modifiers {
		if m.Attribute == attribute && m.Operation == AttributeAddValue &&
			(slot == "" || SlotGroupHolds(m.Slot, slot)) {
			sum += m.Amount
		}
	}
	return sum
}

// FoodOf returns what a meal of the item restores, if the item is one. The
// eating that consumes it is the caller's to do.
func FoodOf(item int32) (Food, bool) {
	if food := propsOf(item).Food; food != nil {
		return *food, true
	}
	return Food{}, false
}

// CorrectForDrops reports whether the item is the correct tool for a
// block's drops: the first covering rule with correct_for_drops decides, and
// with no rule (bare hands) it is not.
// vanilla: Tool.isCorrectForDrops
func CorrectForDrops(item int32, b Block) bool {
	if b >= BlockCount {
		return false
	}
	tool := propsOf(item).Tool
	if tool == nil {
		return false
	}
	for _, rule := range tool.Rules {
		if (rule.CorrectForDrops || rule.DeniesDrops) && rule.Blocks.Contains(b) {
			return rule.CorrectForDrops
		}
	}
	return false
}

// MiningSpeed returns the speed before enchantments and effects: the first
// covering rule's speed, else the tool's default, else 1.
// vanilla: Item.getDestroySpeed, Tool.getMiningSpeed
func MiningSpeed(item int32, b Block) float32 {
	tool := propsOf(item).Tool
	if tool == nil {
		return 1
	}
	if b >= BlockCount {
		return tool.DefaultMiningSpeed
	}
	for _, rule := range tool.Rules {
		if rule.Speed != 0 && rule.Blocks.Contains(b) {
			return rule.Speed
		}
	}
	return tool.DefaultMiningSpeed
}

// RequiresCorrectTool reports a block's requiresCorrectToolForDrops, read off
// the server by tools/gen blockdrops.
func RequiresCorrectTool(b Block) bool {
	return b < BlockCount && blockRequiresCorrectTool[b]
}

// Hardness returns a block's destroy time: -1 for one no survival dig breaks
// (bedrock). False for air and a block this version does not name.
// vanilla: BlockState.getDestroySpeed
func Hardness(b Block) (float32, bool) {
	if b >= BlockCount || !blockHasHardness[b] {
		return 0, false
	}
	return blockHardness[b], true
}

// AttackSpeed returns the main-hand attack-speed modifier, on top of the
// attribute's base; 0 without one.
func AttackSpeed(item int32) float32 {
	return float32(addedTo(item, AttributeAttackSpeed, "mainhand"))
}

// RepairsWith reports whether an anvil repairs item with other, per
// minecraft:repairable: "#tag" asks the item tags, a name the item
// registry; no component repairs with nothing.
// vanilla: AnvilMenu.createResult (isValidRepairItem)
func RepairsWith(item, other int32) bool {
	ref := propsOf(item).Repairable
	if ref == "" {
		return false
	}
	if tag, ok := strings.CutPrefix(ref, "#"); ok {
		return Item(other).InTag(ItemTagOf(tag))
	}
	name, ok := ItemName(other)
	return ok && name == ref
}

// IsBundle reports whether the item has bundle contents by default.
func IsBundle(item int32) bool {
	switch Item(item) {
	case ItemBundle, ItemWhiteBundle, ItemOrangeBundle, ItemMagentaBundle,
		ItemLightBlueBundle, ItemYellowBundle, ItemLimeBundle, ItemPinkBundle,
		ItemGrayBundle, ItemLightGrayBundle, ItemCyanBundle, ItemPurpleBundle,
		ItemBlueBundle, ItemBrownBundle, ItemGreenBundle, ItemRedBundle, ItemBlackBundle:
		return true
	}
	return false
}
