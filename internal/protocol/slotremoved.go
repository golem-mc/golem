package protocol

// Removals is the set of item-default components a stack's patch removes,
// one bit per entry of removable.
type Removals uint64

// removable is the components whose removal a stack keeps, because the
// server reads them through the stack; any other removal only drops a raw
// value. Names are the registry keys a saved or commanded patch writes.
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
}

// Removals has 64 bits; this fails to compile when removable outgrows it.
var _ [64 - len(removable)]struct{}

// removalBit is component's bit in Removals, zero for one not removable.
func removalBit(component int32) Removals {
	for i, r := range removable {
		if r.Type == component {
			return 1 << i
		}
	}
	return 0
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
	for _, r := range removable {
		if s.Removes(r.Type) {
			names = append(names, r.Name)
		}
	}
	return names
}

// Removes reports whether the stack's patch removes component from its
// item's defaults.
func (s ItemStack) Removes(component int32) bool {
	return s.Removed&removalBit(component) != 0
}

// Remove takes component away entirely, the removal rather than a return to
// its item's default; false, leaving the stack as it was, for a component
// whose removal the stack cannot keep.
func (s *ItemStack) Remove(component int32) bool {
	bit := removalBit(component)
	if bit == 0 {
		return false
	}
	s.clearRaw(component)
	s.NotePatch(component)
	s.Removed |= bit
	return true
}

// ResetComponent returns component to its item's default: its own value and
// its removal are both gone, and the patch no longer lists it.
// vanilla: PatchedDataComponentMap.applyPatch
func (s *ItemStack) ResetComponent(component int32) {
	s.dropRaw(component)
	s.Removed &^= removalBit(component)
	s.ForgetPatch(component)
}
