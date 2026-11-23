package protocol

import "math/bits"

// Removals is the set of item-default components a stack's patch removes,
// one bit per entry of removable.
type Removals [2]uint64

// removable lists the removals a stack keeps in its patch. Names are the
// registry keys a saved or commanded patch writes.
var removable = [...]struct {
	Type int32
	Name string
}{
	{DataComponentTool, "minecraft:tool"},
	{DataComponentAttackRange, "minecraft:attack_range"},
	{DataComponentEquippable, "minecraft:equippable"},
	{DataComponentWeapon, "minecraft:weapon"},
	{DataComponentBlocksAttacks, "minecraft:blocks_attacks"},
	{DataComponentFood, "minecraft:food"},
	{DataComponentConsumable, "minecraft:consumable"},
	{DataComponentUseRemainder, "minecraft:use_remainder"},
	{DataComponentUseCooldown, "minecraft:use_cooldown"},
	{DataComponentGlider, "minecraft:glider"},
	{DataComponentDeathProtection, "minecraft:death_protection"},
	{DataComponentDamageResistant, "minecraft:damage_resistant"},
	{DataComponentJukeboxPlayable, "minecraft:jukebox_playable"},
	{DataComponentOminousBottleAmplifier, "minecraft:ominous_bottle_amplifier"},
	{DataComponentBlockTransformer, "minecraft:block_transformer"},
	{DataComponentCompostable, "minecraft:compostable"},
	{DataComponentCookingFuel, "minecraft:cooking_fuel"},
	{DataComponentBrewingFuel, "minecraft:brewing_fuel"},
	{DataComponentMaxStackSize, "minecraft:max_stack_size"},
	{DataComponentUseEffects, "minecraft:use_effects"},
	{DataComponentPiercingWeapon, "minecraft:piercing_weapon"},
	{DataComponentKineticWeapon, "minecraft:kinetic_weapon"},
	{DataComponentAttackAnimation, "minecraft:attack_animation"},
	{DataComponentInteractAnimation, "minecraft:interact_animation"},
	{DataComponentVillagerFood, "minecraft:villager_food"},
	{DataComponentMobVisibility, "minecraft:mob_visibility"},
	{DataComponentMaxDamage, "minecraft:max_damage"},
	{DataComponentMinimumAttackCharge, "minecraft:minimum_attack_charge"},
	{DataComponentDamageType, "minecraft:damage_type"},
	{DataComponentEnchantable, "minecraft:enchantable"},
	{DataComponentRepairable, "minecraft:repairable"},
	{DataComponentDye, "minecraft:dye"},
	{DataComponentProvidesTrimMaterial, "minecraft:provides_trim_material"},
	{DataComponentProvidesBannerPatterns, "minecraft:provides_banner_patterns"},
	{DataComponentAttributeModifiers, "minecraft:attribute_modifiers"},
	{DataComponentBreakSound, "minecraft:break_sound"},
	{DataComponentEnchantments, "minecraft:enchantments"},
	{DataComponentItemModel, "minecraft:item_model"},
	{DataComponentItemName, "minecraft:item_name"},
	{DataComponentLore, "minecraft:lore"},
	{DataComponentRarity, "minecraft:rarity"},
	{DataComponentRepairCost, "minecraft:repair_cost"},
	{DataComponentTooltipDisplay, "minecraft:tooltip_display"},
	{DataComponentDamage, "minecraft:damage"},
}

var _ [len(Removals{})*64 - len(removable)]struct{}

// removalIndexes stores one-based indexes so unsupported components read as zero.
var removalIndexes = func() [DataComponentTypeCount]uint8 {
	var indexes [DataComponentTypeCount]uint8
	for i, r := range removable {
		indexes[r.Type] = uint8(i + 1)
	}
	return indexes
}()

func removalIndex(component int32) int {
	if component < 0 || component >= DataComponentTypeCount {
		return -1
	}
	return int(removalIndexes[component]) - 1
}

func (r Removals) has(index int) bool {
	if index < 0 {
		return false
	}
	return r[index/64]&(uint64(1)<<(index%64)) != 0
}

func (r Removals) count() int32 {
	return int32(bits.OnesCount64(r[0]) + bits.OnesCount64(r[1]))
}

// RemovableType returns the type of the removable component named name (a
// qualified registry key), false for any other.
func RemovableType(name string) (int32, bool) {
	for _, r := range removable {
		if r.Name == name {
			return r.Type, true
		}
	}
	return 0, false
}

// RemovedNames returns the registry keys of the components the stack's
// patch removes, in removable's order.
func (s ItemStack) RemovedNames() []string {
	var names []string
	for i, r := range removable {
		if s.Removed.has(i) {
			names = append(names, r.Name)
		}
	}
	return names
}

// Removes reports whether the stack's patch removes component from its
// item's defaults.
func (s ItemStack) Removes(component int32) bool {
	return s.removals().has(removalIndex(component))
}

// removals is Removed as the patch carries it: a worn stack has a damage
// value, which replaces a removal of it.
func (s ItemStack) removals() Removals {
	if s.Damage > 0 {
		index := removalIndex(DataComponentDamage)
		s.Removed[index/64] &^= uint64(1) << (index % 64)
	}
	return s.Removed
}

// Remove takes component away entirely, the removal rather than a return to
// its item's default; false, leaving the stack as it was, for a component
// whose removal the stack cannot keep.
func (s *ItemStack) Remove(component int32) bool {
	index := removalIndex(component)
	if index < 0 {
		return false
	}
	s.clearComponent(component)
	s.NotePatch(component)
	s.Removed[index/64] |= uint64(1) << (index % 64)
	return true
}

// ResetComponent returns component to its item's default.
// vanilla: PatchedDataComponentMap.applyPatch
func (s *ItemStack) ResetComponent(component int32) {
	s.clearComponent(component)
	s.clearRemoval(component)
	s.ForgetPatch(component)
}

func (s *ItemStack) clearComponent(component int32) {
	switch component {
	case DataComponentDamage:
		s.Damage = 0
	case DataComponentRepairCost:
		s.RepairCost = 0
	case DataComponentEnchantments:
		s.Enchantments, s.enchantmentOrder = nil, nil
	}
	s.clearRaw(component)
}

func (s *ItemStack) clearRemoval(component int32) {
	if index := removalIndex(component); index >= 0 {
		s.Removed[index/64] &^= uint64(1) << (index % 64)
	}
}

// SetDamage writes the item's wear, restoring the component if it was removed.
func (s *ItemStack) SetDamage(damage int32) {
	s.Damage = damage
	s.clearRemoval(DataComponentDamage)
	if damage > 0 {
		s.NotePatch(DataComponentDamage)
	} else {
		s.ForgetPatch(DataComponentDamage)
	}
}

// SetRepairCost writes the anvil work, restoring the component if it was removed.
func (s *ItemStack) SetRepairCost(cost int32) {
	s.RepairCost = cost
	s.clearRemoval(DataComponentRepairCost)
	if cost > 0 {
		s.NotePatch(DataComponentRepairCost)
	} else {
		s.ForgetPatch(DataComponentRepairCost)
	}
}
