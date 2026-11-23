package registry

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The stack codec: one item stack in a file's NBT (player slots, container
// Items, an item entity's Item) as a compound of the item's name, its count
// and, under "components", the components that differ from the item's
// defaults, each in its codec's shape.
//
// Components held only as raw wire bytes (protocol.ItemStack's Raw) are not
// saved: their registry IDs would tie the file to this version. The
// exceptions are written in their own NBT shape because a file can name
// everything in them (bundle and container contents, potions, lodestone
// trackers, block_state, lore, attribute modifiers, custom data, map ids, books, banner and shield layers, profiles,
// dyed colours, stew effects, armour trims, instruments, painting and mob
// variants, potion duration scales, item models, tooltip styles, rarity,
// tooltip display, break sounds, can_place_on and can_break, entity_data,
// block_entity_data and equippables).
//
// This lives in registry because the chunk and player-file packages both
// keep stacks and must not depend on each other. The shapes were read off
// files a vanilla server wrote.

// StackTag writes one stack as a file stores it. The caller has checked the
// item is one this version names.
func StackTag(stack protocol.ItemStack) *nbt.OrderedCompound {
	entry := nbt.NewOrderedCompound(3)
	putStackTag(entry, stack)
	return entry
}

// templateTag writes ItemStackTemplate.MAP_CODEC: a count of 1 is left out.
func templateTag(stack protocol.ItemStack) *nbt.OrderedCompound {
	entry := nbt.NewOrderedCompound(3)
	name, _ := ItemName(stack.ItemID)
	entry.Set("id", nbt.String(name))
	if stack.Count != 1 {
		entry.Set("count", nbt.Int(stack.Count))
	}
	if components := stackComponentsTag(stack); components != nil {
		entry.Set("components", components)
	}
	return entry
}

// SlottedStackTag writes ItemStackWithSlot.CODEC, inserting Slot before the stack fields.
func SlottedStackTag(slot int, stack protocol.ItemStack) *nbt.OrderedCompound {
	entry := nbt.NewOrderedCompound(4)
	entry.Set("Slot", nbt.Byte(slot))
	putStackTag(entry, stack)
	return entry
}

// vanilla: ItemStack.MAP_CODEC
func putStackTag(entry nbt.CompoundTag, stack protocol.ItemStack) {
	name, _ := ItemName(stack.ItemID)
	entry.Set("id", nbt.String(name))
	entry.Set("count", nbt.Int(stack.Count))
	if components := stackComponentsTag(stack); components != nil {
		entry.Set("components", components)
	}
}

// PutSlottedItems writes a container's filled slots under tag's "Items",
// each stack with its Slot. With alsoWhenEmpty an all-empty container keeps
// an empty list, else it writes none.
// vanilla: ItemStackWithSlot.CODEC, ContainerHelper.saveAllItems
func PutSlottedItems(tag nbt.CompoundTag, slots []protocol.ItemStack, alsoWhenEmpty bool) {
	items := make([]nbt.Tag, 0, len(slots))
	for slot, stack := range slots {
		if stack.Empty() {
			continue
		}
		items = append(items, SlottedStackTag(slot, stack))
	}
	if len(items) > 0 || alsoWhenEmpty {
		tag.Set("Items", nbt.List{Elem: nbt.TypeCompound, Items: items})
	}
}

// LoadSlottedItems fills slots from tag's "Items", skipping an entry whose
// Slot is past the slots or whose stack does not read; a missing Slot is 0.
// The Slot is an unsigned byte, so -1 is 255. vanilla: ItemStackWithSlot.CODEC
func LoadSlottedItems(tag nbt.Compound, slots []protocol.ItemStack) {
	items, _ := nbt.Get[nbt.List](tag, "Items")
	for _, entry := range items.Items {
		item, ok := nbt.CompoundView(entry)
		if !ok {
			continue
		}
		slot, _ := nbt.ByteValue(item["Slot"])
		if int(uint8(slot)) >= len(slots) {
			continue
		}
		if stack, ok := StoredContainerStack(item); ok {
			slots[uint8(slot)] = stack
		}
	}
}

// StackComponents is stackComponentsTag's entries, for callers that index
// them by name; nil when there are none, the key then left out.
func StackComponents(stack protocol.ItemStack) nbt.Compound {
	if tag := stackComponentsTag(stack); tag != nil {
		return tag.Entries()
	}
	return nil
}

// stackComponentsTag writes the components a stack carries away from its
// item's defaults in the stack's patch order, which for a stack with no
// history is the hash order of the keys; nil when there are none.
// vanilla: DataComponentPatch.CODEC
func stackComponentsTag(stack protocol.ItemStack) *nbt.OrderedCompound {
	components := stackComponentsMap(stack)
	if components == nil {
		return nil
	}
	byID := make(map[int32]string, len(components))
	var ids []int32
	var unnamed []string
	for _, name := range nbt.HashOrderedKeys(components) {
		id, ok := ComponentID(strings.TrimPrefix(name, "!"))
		if !ok {
			unnamed = append(unnamed, name)
			continue
		}
		byID[id] = name
		ids = append(ids, id)
	}
	tag := nbt.NewOrderedCompound(len(components))
	for _, id := range stack.PatchOrdered(ids) {
		tag.Set(byID[id], components[byID[id]])
	}
	for _, name := range unnamed {
		tag.Set(name, components[name])
	}
	return tag
}

// stackComponentsMap collects the component entries of a stack by name.
func stackComponentsMap(stack protocol.ItemStack) nbt.Compound {
	var components nbt.Compound
	if stack.Damage > 0 {
		components = nbt.Compound{}
		components["minecraft:damage"] = nbt.Int(stack.Damage)
	}
	components = putUnit(components, "minecraft:unbreakable", stack.Unbreakable)
	components = putUnit(components, "minecraft:intangible_projectile", stack.Intangible)
	if name, ok := stack.ItemNameOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:item_name"] = protocol.TextTag(name)
	}
	if stack.CustomName != nil {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:custom_name"] = protocol.TextTag(*stack.CustomName)
	}
	if len(stack.Enchantments) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:enchantments"] = enchantmentsTag(stack, false)
	}
	if stack.RepairCost > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:repair_cost"] = nbt.Int(stack.RepairCost)
	}
	switch stack.GlintOverride {
	case protocol.GlintShown, protocol.GlintHidden:
		if components == nil {
			components = nbt.Compound{}
		}
		// Written either way (1 shown, 0 hidden): its absence would leave a
		// default glint standing.
		shown := nbt.Byte(0)
		if stack.GlintOverride == protocol.GlintShown {
			shown = 1
		}
		components["minecraft:enchantment_glint_override"] = shown
	}
	if stack.Lock != nil {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:lock"] = LockTag(stack.Lock)
	}
	if len(stack.DebugStick) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:debug_stick_state"] = stack.DebugStickTag()
	}
	if len(stack.Recipes) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		names := make([]nbt.Tag, len(stack.Recipes))
		for i, name := range stack.Recipes {
			names[i] = nbt.String(name)
		}
		components["minecraft:recipes"] = nbt.List{Elem: nbt.TypeString, Items: names}
	}
	if len(stack.StoredEnchantments) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:stored_enchantments"] = enchantmentsTag(stack, true)
	}
	if len(stack.Container) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:container"] = containerTag(stack.Container)
	}
	if lines := stack.LoreOf(); lines != nil {
		if components == nil {
			components = nbt.Compound{}
		}
		tags := make([]nbt.Tag, len(lines))
		for i, line := range lines {
			tags[i] = protocol.TextTag(line)
		}
		components["minecraft:lore"] = nbt.List{Elem: tagElem(tags), Items: tags}
	}
	if entries, ok := stack.AttributeModifiersOf(); ok {
		if tag, ok := attributeModifiersTag(entries); ok {
			if components == nil {
				components = nbt.Compound{}
			}
			components["minecraft:attribute_modifiers"] = tag
		}
	}
	if tag := stack.CustomDataTag(); tag != nil {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:custom_data"] = tag
	}
	if tag := stack.BucketEntityDataTag(); tag != nil {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:bucket_entity_data"] = tag
	}
	if content, ok := stack.SulfurCubeContent(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:sulfur_cube_content"] = templateTag(content)
	}
	if contents := stack.BundleContents(); len(contents) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:bundle_contents"] = bundleContentsTag(contents)
	}
	if loaded := stack.ChargedProjectiles(); len(loaded) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		// The bundle's codec: a list of item templates.
		components["minecraft:charged_projectiles"] = bundleContentsTag(loaded)
	}
	if tag, ok := potionContentsTag(stack); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:potion_contents"] = tag
	}
	if rgb, ok := stack.DyedColorOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:dyed_color"] = nbt.Int(rgb)
	}
	if effects := stack.SuspiciousStewEffectsOf(); len(effects) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		entries := make([]nbt.Tag, 0, len(effects))
		for _, effect := range effects {
			name := MobEffectName(effect.Effect)
			if name == "" {
				continue
			}
			entry := &nbt.OrderedCompound{}
			entry.Set("id", nbt.String(name))
			entry.Set("duration", nbt.Int(effect.Duration))
			entries = append(entries, entry)
		}
		components["minecraft:suspicious_stew_effects"] = nbt.List{Elem: nbt.TypeCompound, Items: entries}
	}
	if tracker, ok := stack.LodestoneTrackerOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:lodestone_tracker"] = lodestoneTrackerTag(tracker)
	}
	if props, ok := stack.BlockStatePropertiesTag(); ok && len(props.Entries()) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:block_state"] = props
	}
	if layers, ok := stack.BannerPatternsOf(); ok && len(layers) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:banner_patterns"] = BannerPatternsTag(layers)
	}
	if dye, ok := stack.BaseColorOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:base_color"] = nbt.String(DyeNames[dye])
	}
	if profile, ok := stack.ProfileTag(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:profile"] = profile
	}
	components = withJukeboxPlayableTag(components, stack)
	components = withBlockTransformerTag(components, stack)
	if sound, ok := stack.NoteBlockSoundOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:note_block_sound"] = nbt.String(sound)
	}
	if sides, ok := stack.PotDecorationsOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:pot_decorations"] = PotDecorationsTag(sides)
	}
	if bees, ok := stack.BeesOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:bees"] = BeesTag(bees)
	}
	for _, side := range [...]struct {
		front bool
		name  string
	}{{true, "minecraft:sign_text_front"}, {false, "minecraft:sign_text_back"}} {
		if text, ok := stack.SignTextOf(side.front); ok {
			if components == nil {
				components = nbt.Compound{}
			}
			components[side.name] = SignTextTag(text)
		}
	}
	components = putUnit(components, "minecraft:waxed", stack.Waxed())
	if len(stack.MapDecorations) > 0 {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:map_decorations"] = stack.MapDecorationsTag()
	}
	if id, ok := stack.MapIDOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:map_id"] = nbt.Int(id)
	}
	if amplifier, ok := stack.OminousBottleAmplifier(); ok && slices.ContainsFunc(stack.Raw, func(raw protocol.RawComponent) bool {
		return raw.Type == protocol.DataComponentOminousBottleAmplifier
	}) {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:ominous_bottle_amplifier"] = nbt.Int(amplifier)
	}
	if d, ok := stack.CustomModelDataOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:custom_model_data"] = customModelDataTag(d)
	}
	if stack.ContainerLoot.Table != "" {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:container_loot"] = containerLootTag(stack.ContainerLoot)
	}
	if book, ok := stack.WritableBookOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:writable_book_content"] = writableBookTag(book)
	}
	if book, ok := stack.WrittenBookOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:written_book_content"] = writtenBookTag(book)
	}
	if e, ok := stack.EquippableOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:equippable"] = equippableTag(e)
	}
	if tool, ok := stack.ToolOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:tool"] = toolTag(tool)
	}
	components = withConsumableTags(components, stack)
	components = withCompostableTag(components, stack)
	components = withFuelTags(components, stack)
	components = withRecordTags(components, stack)
	components = withMaxStackSizeTag(components, stack)
	components = withPropTags(components, stack)
	components = withBlocksAndWeaponTags(components, stack)
	components = withAttackRangeTag(components, stack)
	components = withProtectionTags(components, stack)
	for _, name := range stack.RemovedNames() {
		if components == nil {
			components = nbt.Compound{}
		}
		// A removal is '!' plus the key, with an empty map as the value.
		// vanilla: DataComponentPatch.PatchKey.CODEC
		components["!"+name] = nbt.Compound{}
	}
	if tag, ok := fireworksTag(stack); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:fireworks"] = tag
	}
	if e, ok := stack.FireworkExplosionOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:firework_explosion"] = fireworkExplosionTag(e)
	}
	return addDisplayComponents(stack, putAdventureAndEntityData(stack, putVariantComponents(stack, components)))
}

// putUnit sets a unit-valued component when set: an empty compound, whose
// presence is the whole value. It returns the map, made if it was nil.
func putUnit(components nbt.Compound, name string, set bool) nbt.Compound {
	if !set {
		return components
	}
	if components == nil {
		components = nbt.Compound{}
	}
	components[name] = nbt.Compound{}
	return components
}

// bundleContentsTag writes a bundle_contents list, one stack per entry in
// the bundle's order.
func bundleContentsTag(contents []protocol.ItemStack) nbt.List {
	items := make([]nbt.Tag, 0, len(contents))
	for _, entry := range contents {
		items = append(items, templateTag(entry))
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// potionContentsTag writes a stack's potion_contents; false when it carries
// none or names a potion or effect this version's registry lacks.
// vanilla: PotionContents.CODEC
func potionContentsTag(stack protocol.ItemStack) (*nbt.OrderedCompound, bool) {
	contents, ok := stack.PotionContentsOf()
	if !ok {
		return nil, false
	}
	return PotionContentsTag(contents)
}

// PotionContentsTag writes potion contents in their NBT codec shape; false
// when they name a potion or effect this version's registry lacks.
// vanilla: PotionContents.CODEC
func PotionContentsTag(contents protocol.PotionContents) (*nbt.OrderedCompound, bool) {
	tag := &nbt.OrderedCompound{}
	if contents.Potion != protocol.NoPotion {
		name, ok := PotionName(contents.Potion)
		if !ok {
			return nil, false
		}
		tag.Set("potion", nbt.String(name))
	}
	if contents.CustomColor != nil {
		tag.Set("custom_color", nbt.Int(*contents.CustomColor))
	}
	if len(contents.CustomEffects) > 0 {
		// The codec reads the copying accessor, which drops hidden effects.
		// vanilla: PotionContents.customEffects
		visible := make([]protocol.PotionEffectSpec, len(contents.CustomEffects))
		copy(visible, contents.CustomEffects)
		for i := range visible {
			visible[i].Hidden = nil
		}
		effects, ok := potionEffectsTag(visible)
		if !ok {
			return nil, false
		}
		tag.Set("custom_effects", effects)
	}
	if contents.CustomName != nil {
		tag.Set("custom_name", nbt.String(*contents.CustomName))
	}
	return tag, true
}

// potionEffectsTag writes a custom_effects list.
func potionEffectsTag(specs []protocol.PotionEffectSpec) (nbt.List, bool) {
	items := make([]nbt.Tag, 0, len(specs))
	for i := range specs {
		entry, ok := potionEffectTag(&specs[i])
		if !ok {
			return nbt.List{}, false
		}
		items = append(items, entry)
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}, true
}

// lodestoneTrackerTag writes the target only when there is one (dimension
// name, block as three ints), and tracked only away from its default.
// vanilla: LodestoneTracker.CODEC, GlobalPos.MAP_CODEC, BlockPos.CODEC
func lodestoneTrackerTag(tracker protocol.LodestoneTracker) *nbt.OrderedCompound {
	tag := &nbt.OrderedCompound{}
	if tracker.Target != nil {
		target := &nbt.OrderedCompound{}
		target.Set("dimension", nbt.String(tracker.Target.Dimension))
		target.Set("pos", nbt.IntArray{tracker.Target.X, tracker.Target.Y, tracker.Target.Z})
		tag.Set("target", target)
	}
	if !tracker.Tracked {
		tag.Set("tracked", nbt.Byte(0))
	}
	return tag
}

// potionEffectTag writes one custom effect: its registry name beside the
// details, which the codec also nests nameless as a hidden effect.
// vanilla: MobEffectInstance.CODEC
func potionEffectTag(spec *protocol.PotionEffectSpec) (*nbt.OrderedCompound, bool) {
	name := MobEffectName(spec.ID)
	if name == "" {
		return nil, false
	}
	tag := &nbt.OrderedCompound{}
	tag.Set("id", nbt.String(name))
	if !putPotionEffectDetails(tag, spec) {
		return nil, false
	}
	return tag, true
}

// vanilla: MobEffectInstance.Details.MAP_CODEC
func putPotionEffectDetails(tag nbt.CompoundTag, spec *protocol.PotionEffectSpec) bool {
	if spec.Amplifier != 0 {
		if spec.Amplifier < 0 || spec.Amplifier > 255 {
			return false
		}
		tag.Set("amplifier", nbt.Byte(spec.Amplifier))
	}
	if spec.Duration != 0 {
		tag.Set("duration", nbt.Int(spec.Duration))
	}
	if spec.Ambient {
		tag.Set("ambient", nbt.Byte(1))
	}
	if !spec.ShowParticles {
		tag.Set("show_particles", nbt.Byte(0))
	}
	icon := nbt.Byte(0)
	if spec.ShowIcon {
		icon = 1
	}
	// A missing show_icon reads as show_particles, so always encode it.
	tag.Set("show_icon", icon)
	if spec.Hidden != nil {
		hidden := &nbt.OrderedCompound{}
		if !putPotionEffectDetails(hidden, spec.Hidden) {
			return false
		}
		tag.Set("hidden_effect", hidden)
	}
	return true
}

// containerTag writes a container component: one entry per filled slot,
// naming the slot and the stack.
func containerTag(contents []protocol.ItemStack) nbt.List {
	var slots []nbt.Tag
	for i, entry := range contents {
		if entry.Empty() {
			continue
		}
		slot := &nbt.OrderedCompound{}
		slot.Set("slot", nbt.Int(i))
		slot.Set("item", templateTag(entry))
		slots = append(slots, slot)
	}
	if slots == nil {
		slots = []nbt.Tag{}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: slots}
}

// recipeKeysOf reads a list of recipe keys, each namespaced as the
// identifier codec reads it. vanilla: Recipe.KEY_CODEC
func recipeKeysOf(value nbt.Tag) ([]string, bool) {
	list, ok := value.(nbt.List)
	if !ok {
		return nil, false
	}
	keys := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		name, ok := item.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(name)) {
			return nil, false
		}
		key := string(name)
		if i := strings.IndexByte(key, ':'); i < 0 {
			key = "minecraft:" + key
		} else if i == 0 {
			key = "minecraft" + key
		}
		keys = append(keys, key)
	}
	return keys, true
}

// enchantmentsTag writes an enchantments component, registry name to level.
// An unresolvable ID, which no stack this server holds can carry, is left
// out rather than written wrong.
func enchantmentsTag(stack protocol.ItemStack, stored bool) *nbt.OrderedCompound {
	levels := stack.Enchantments
	if stored {
		levels = stack.StoredEnchantments
	}
	tag := nbt.NewOrderedCompound(len(levels))
	for _, id := range stack.EnchantmentIDs(stored) {
		if name, ok := EnchantmentName(id); ok {
			tag.Set(name, nbt.Int(levels[id]))
		}
	}
	return tag
}

// StoredStack reads one stack out of a file's compound. It reports false for
// a stack naming an unknown item or carrying a component this server neither
// models nor holds raw; the caller then keeps the compound whole, so what
// vanilla wrote is saved back untouched.
func StoredStack(entry nbt.CompoundTag) (protocol.ItemStack, bool) {
	return storedStack(entry.Entries(), false)
}

// StoredContainerStack reads a container's stack, accepting safe partial
// written book pages while leaving unsupported components to the caller.
// vanilla: ContainerHelper.loadAllItems, TagValueInput.TypedListWrapper.iterator
func StoredContainerStack(entry nbt.CompoundTag) (protocol.ItemStack, bool) {
	return storedStack(entry.Entries(), true)
}

// storedStack permits book page partial values only for container loading.
// vanilla: TagValueInput.TypedListWrapper.iterator
func storedStack(entry nbt.Compound, partial bool) (protocol.ItemStack, bool) {
	components, hasComponents := entry["components"].(nbt.CompoundTag)
	if !hasComponents {
		view, isView := nbt.Get[nbt.Compound](entry, "components")
		components, hasComponents = view, isView
	}
	if hasComponents && len(components.Entries()) == 0 {
		// An empty components compound, which vanilla would not write,
		// reads as the plain stack, as an empty patch does.
		hasComponents = false
	}
	stack, ok := storedStackSans(entry)
	if !ok {
		return protocol.ItemStack{}, false
	}
	if hasComponents {
		// Keys are read in the compound's native order, which the patch keeps.
		for _, name := range nbt.HashOrderedKeys(components) {
			value := components.Entries()[name]
			if !readComponent(&stack, name, value) {
				if partial && Qualify(name) == "minecraft:written_book_content" {
					book, ok := readWrittenBook(value, true)
					if ok && stack.SetWrittenBook(book) {
						continue
					}
				}
				return protocol.ItemStack{}, false
			}
		}
		if !StackComponentsValid(stack) {
			return protocol.ItemStack{}, false
		}
	}
	return stack, true
}

// ReadComponent reads one component in its codec's NBT shape onto the
// stack; false when it is not modelled or the value is refused.
func ReadComponent(stack *protocol.ItemStack, name string, value nbt.Tag) bool {
	return readComponent(stack, name, value)
}

// readComponent reads one component onto a stored stack, reporting whether
// it is modelled in its codec's shape.
func readComponent(stack *protocol.ItemStack, name string, value nbt.Tag) bool {
	// Noted first so a reader that leaves the component at its default can
	// take it back out with ResetComponent.
	if id, named := ComponentID(Qualify(strings.TrimPrefix(name, "!"))); named {
		stack.NotePatch(id)
	}
	ok, _ := readComponentKnown(stack, name, value)
	return ok
}

// ComponentRefused reports whether the stored-stack reader models the named
// component and refuses the value, so /give and a saved stack agree; an
// unmodelled component is never refused.
func ComponentRefused(name string, value nbt.Tag) bool {
	var scratch protocol.ItemStack
	ok, modeled := readComponentKnown(&scratch, name, value)
	return modeled && !ok
}

// readComponentKnown is readComponent that also reports whether the
// component has a reader at all (modeled). A shape with no reading refuses
// the whole stack, so a half-read stack is never saved back half-written.
func readComponentKnown(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	if removed, isRemoval := strings.CutPrefix(name, "!"); isRemoval {
		if component, kept := protocol.RemovableType(Qualify(removed)); kept {
			if entry, isMap := nbt.CompoundView(value); !isMap || len(entry) != 0 {
				return false, true
			}
			stack.Remove(component)
			return true, true
		}
	}
	name = Qualify(name) // an Identifier: namespace defaults to minecraft
	switch name {
	case "minecraft:damage":
		wear, ok := value.(nbt.Int)
		if !ok || wear < 0 {
			return false, true
		}
		stack.Damage = int32(wear)
	case "minecraft:unbreakable", "minecraft:intangible_projectile":
		// A unit: an empty compound, whose presence is the whole value.
		if unit, ok := nbt.CompoundView(value); !ok || len(unit) != 0 {
			return false, true
		}
		stack.Unbreakable = stack.Unbreakable || name == "minecraft:unbreakable"
		stack.Intangible = stack.Intangible || name == "minecraft:intangible_projectile"
	case "minecraft:item_name":
		text, ok := protocol.StyledTextOf(value)
		if !ok {
			return false, true
		}
		stack.SetItemName(text)
	case "minecraft:custom_name":
		text, ok := protocol.StyledTextOf(value)
		if !ok {
			return false, true
		}
		stack.CustomName = &text
	case "minecraft:lock":
		lock, ok := value.(nbt.CompoundTag)
		if !ok {
			return false, true
		}
		stack.Lock = nbt.Copy(lock).(nbt.CompoundTag)
	case "minecraft:debug_stick_state":
		compound, ok := nbt.CompoundView(value)
		if !ok {
			return false, true
		}
		state := make(map[string]string, len(compound))
		for block, tag := range compound {
			prop, ok := tag.(nbt.String)
			if _, has := BlockPropertyOrder(block, string(prop)); !ok || !has {
				return false, true
			}
			state[block] = string(prop)
		}
		stack.SetDebugStickState(state, nbt.HashOrderedKeys(value.(nbt.CompoundTag)))
	case "minecraft:recipes":
		recipes, ok := recipeKeysOf(value)
		if !ok {
			return false, true
		}
		stack.Recipes = recipes
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		levels, ok := enchantmentsOf(value)
		if !ok {
			return false, true
		}
		ids := make([]int32, 0, len(levels))
		for _, key := range nbt.HashOrderedKeys(value.(nbt.CompoundTag)) {
			id, _ := EnchantmentID(key)
			ids = append(ids, id)
		}
		stack.SetEnchantments(name == "minecraft:stored_enchantments", levels, ids...)
	case "minecraft:repair_cost":
		cost, ok := value.(nbt.Int)
		if !ok || cost < 0 {
			return false, true
		}
		stack.RepairCost = int32(cost)
	case "minecraft:enchantment_glint_override":
		glint, ok := nbt.IntValue(value)
		if !ok {
			return false, true
		}
		// Exactly 2 is golem's earlier encoding of hidden; keep such saved items hidden.
		if glint != 0 && glint != 2 {
			stack.GlintOverride = protocol.GlintShown
		} else {
			stack.GlintOverride = protocol.GlintHidden
		}
	case "minecraft:container":
		contents, ok := containerOf(value)
		if !ok {
			return false, true
		}
		stack.Container = contents
	case "minecraft:bundle_contents":
		contents, ok := bundleContentsOf(value)
		if !ok {
			return false, true
		}
		stack.SetBundleContents(contents)
	case "minecraft:lore":
		list, ok := value.(nbt.List)
		if !ok || len(list.Items) > 256 {
			return false, true
		}
		lines := make([]protocol.Text, 0, len(list.Items))
		for _, item := range list.Items {
			line, ok := protocol.StyledTextOf(item)
			if !ok {
				return false, true
			}
			lines = append(lines, line)
		}
		stack.SetLore(lines)
	case "minecraft:attribute_modifiers":
		entries, ok := attributeModifiersOf(value)
		if !ok {
			return false, true
		}
		stack.SetAttributeModifiers(entries)
	case "minecraft:custom_data":
		tag, ok := value.(nbt.CompoundTag)
		if !ok {
			return false, true
		}
		stack.SetCustomDataTag(tag)
	case "minecraft:bucket_entity_data":
		tag, ok := compoundTagOrSNBT(value)
		if !ok {
			return false, true
		}
		stack.SetBucketEntityData(tag)
	case "minecraft:sulfur_cube_content":
		tag, ok := nbt.CompoundView(value)
		if !ok {
			return false, true
		}
		content, ok := StoredStack(tag)
		if !ok || content.Empty() {
			return false, true
		}
		stack.SetSulfurCubeContent(content)
	case "minecraft:charged_projectiles":
		loaded, ok := bundleContentsOf(value)
		if !ok {
			return false, true
		}
		stack.SetChargedProjectiles(loaded)
	case "minecraft:potion_contents":
		contents, ok := PotionContentsOf(value)
		if !ok {
			return false, true
		}
		stack.SetPotionContents(contents)
	case "minecraft:dyed_color":
		rgb, ok := dyedColorOf(value)
		if !ok {
			return false, true
		}
		stack.SetDyedColor(rgb)
	case "minecraft:suspicious_stew_effects":
		effects, ok := stewEffectsOf(value)
		if !ok {
			return false, true
		}
		stack.SetSuspiciousStewEffects(effects)
	case "minecraft:lodestone_tracker":
		tracker, ok := lodestoneTrackerOf(value)
		if !ok {
			return false, true
		}
		stack.SetLodestoneTracker(tracker)
	case "minecraft:block_state":
		props, ok := BlockStatePropertiesTag(value)
		if !ok {
			return false, true
		}
		// An empty component is the item's default and reads as absent.
		if len(props) > 0 {
			stack.SetBlockStatePropertiesTag(value.(nbt.CompoundTag))
		}
	case "minecraft:banner_patterns":
		list, ok := value.(nbt.List)
		if !ok {
			return false, true
		}
		layers, ok := BannerPatternsOf(list)
		if !ok || !stack.SetBannerPatterns(layers) {
			return false, true
		}
	case "minecraft:base_color":
		name, ok := value.(nbt.String)
		if !ok {
			return false, true
		}
		dye, ok := dyeColorIDs[string(name)]
		if !ok || !stack.SetBaseColor(dye) {
			return false, true
		}
	case "minecraft:profile":
		var profile nbt.Compound
		switch v := value.(type) {
		case nbt.CompoundTag:
			profile = v.Entries()
		case nbt.String:
			// The string form names the player only.
			profile = nbt.Compound{"name": v}
		default:
			return false, true
		}
		bytes, ok := protocol.ProfileFromTag(profile)
		if !ok {
			return false, true
		}
		stack.Raw = append(slices.Clone(stack.Raw), protocol.RawComponent{Type: protocol.DataComponentProfile, Data: bytes})
	case "minecraft:jukebox_playable":
		return readJukeboxPlayable(stack, value), true
	case "minecraft:note_block_sound":
		name, ok := value.(nbt.String)
		if !ok || !stack.SetNoteBlockSound(string(name)) {
			return false, true
		}
	case "minecraft:pot_decorations":
		tag, ok := nbt.CompoundView(value)
		if !ok {
			return false, true
		}
		sides, ok := PotDecorationsOf(tag)
		if !ok || !stack.SetPotDecorations(sides) {
			return false, true
		}
	case "minecraft:bees":
		list, ok := value.(nbt.List)
		if !ok {
			return false, true
		}
		bees, ok := BeesOf(list)
		if !ok || !stack.SetBees(bees) {
			return false, true
		}
	case "minecraft:sign_text_front", "minecraft:sign_text_back":
		// A non-compound reads as one with no messages, refused.
		tag, _ := nbt.CompoundView(value)
		text, ok := SignTextOf(tag)
		if !ok {
			return false, true
		}
		stack.SetSignText(name == "minecraft:sign_text_front", text)
	case "minecraft:waxed":
		// vanilla: Unit.CODEC, an empty compound
		if _, ok := nbt.CompoundView(value); !ok {
			return false, true
		}
		stack.SetWaxed()
	case "minecraft:map_decorations":
		if !stack.SetMapDecorations(value) {
			return false, true
		}
	case "minecraft:map_id":
		id, ok := nbt.IntValue(value)
		if !ok {
			return false, true
		}
		stack.SetMapID(id)
	case "minecraft:ominous_bottle_amplifier":
		amplifier, ok := nbt.IntValue(value)
		if !ok || amplifier < 0 || amplifier > 4 {
			return false, true
		}
		stack.SetOminousBottleAmplifier(amplifier)
	case "minecraft:writable_book_content":
		book, ok := writableBookOf(value)
		if !ok || !stack.SetWritableBook(book) {
			return false, true
		}
	case "minecraft:written_book_content":
		book, ok := writtenBookOf(value)
		if !ok || !stack.SetWrittenBook(book) {
			return false, true
		}
	case "minecraft:tool", "minecraft:blocks_attacks", "minecraft:weapon", "minecraft:attack_range":
		return readCombatComponent(stack, name, value), true
	case "minecraft:equippable":
		raw, ok := equippableOf(value)
		if !ok {
			return false, true
		}
		stack.SetRaw(raw)
	case "minecraft:fireworks":
		flight, explosions, ok := fireworksOf(value)
		if !ok {
			return false, true
		}
		stack.SetRaw(protocol.FireworksRawComponent(flight, explosions))
	case "minecraft:firework_explosion":
		e, ok := fireworkExplosionOf(value)
		if !ok {
			return false, true
		}
		stack.SetRaw(e.RawComponent())
	default:
		return readVariantComponent(stack, name, value)
	}
	return true, true
}

// containerOf reads a container component's per-slot entries into contents
// up to the last filled slot. An empty entry, a slot past the codec's bound
// or a refused stack makes the whole component unholdable.
func containerOf(value nbt.Tag) ([]protocol.ItemStack, bool) {
	list, ok := value.(nbt.List)
	if !ok || list.Elem != nbt.TypeCompound {
		return nil, false
	}
	if len(list.Items) == 0 {
		// An empty component is its own default, and reads as absent.
		return nil, true
	}
	if len(list.Items) > 256 {
		return nil, false
	}
	length := 0
	contents := make([]protocol.ItemStack, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		slot, ok := nbt.Get[nbt.Int](entry, "slot")
		if !ok || slot < 0 || slot > 255 {
			return nil, false
		}
		inner, ok := nbt.Get[nbt.Compound](entry, "item")
		if !ok {
			return nil, false
		}
		stack, ok := StoredStack(inner)
		if !ok {
			return nil, false
		}
		for len(contents) <= int(slot) {
			contents = append(contents, protocol.ItemStack{})
		}
		contents[slot] = stack
		if int(slot)+1 > length {
			length = int(slot) + 1
		}
	}
	return contents[:length], true
}

// bundleContentsOf reads a bundle_contents list in the bundle's order, each
// stack one the stored reading must accept; empty reads as absent.
func bundleContentsOf(value nbt.Tag) ([]protocol.ItemStack, bool) {
	list, ok := value.(nbt.List)
	if !ok || list.Elem != nbt.TypeCompound {
		return nil, false
	}
	if len(list.Items) == 0 {
		return nil, true
	}
	contents := make([]protocol.ItemStack, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		stack, ok := StoredStack(entry)
		if !ok {
			return nil, false
		}
		contents = append(contents, stack)
	}
	return contents, true
}

// PotionContentsOf reads a potion_contents component: the full compound or,
// as the codec's alternative, a potion name alone. An unknown registry name
// or a field of the wrong type refuses the component; an unknown key is
// skipped.
// vanilla: PotionContents.CODEC
func PotionContentsOf(value nbt.Tag) (protocol.PotionContents, bool) {
	if text, ok := value.(nbt.String); ok {
		id, ok := PotionID(string(text))
		if !ok {
			return protocol.PotionContents{}, false
		}
		return protocol.PotionContents{Potion: id}, true
	}
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.PotionContents{}, false
	}
	contents := protocol.PotionContents{Potion: protocol.NoPotion}
	for name, field := range entry {
		switch name {
		case "potion":
			text, ok := field.(nbt.String)
			if !ok {
				return protocol.PotionContents{}, false
			}
			id, ok := PotionID(string(text))
			if !ok {
				return protocol.PotionContents{}, false
			}
			contents.Potion = id
		case "custom_color":
			c, ok := nbt.IntValue(field)
			if !ok {
				return protocol.PotionContents{}, false
			}
			contents.CustomColor = &c
		case "custom_effects":
			effects, ok := potionEffectsOf(field)
			if !ok {
				return protocol.PotionContents{}, false
			}
			contents.CustomEffects = effects
		case "custom_name":
			text, ok := field.(nbt.String)
			if !ok {
				return protocol.PotionContents{}, false
			}
			custom := string(text)
			contents.CustomName = &custom
		default:
			// The codec skips keys it does not know.
		}
	}
	return contents, true
}

// lodestoneTrackerOf reads an optional "target" (dimension identifier,
// block as three ints) and an optional "tracked" (default true, any number
// reading as the codec's boolean). An unknown field, a wrong shape or a
// half target refuses the component.
// vanilla: LodestoneTracker.CODEC, Codec.BOOL
func lodestoneTrackerOf(value nbt.Tag) (protocol.LodestoneTracker, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.LodestoneTracker{}, false
	}
	tracker := protocol.LodestoneTracker{Tracked: true}
	for name, field := range entry {
		switch name {
		case "target":
			target, ok := lodestoneTargetOf(field)
			if !ok {
				return protocol.LodestoneTracker{}, false
			}
			tracker.Target = target
		case "tracked":
			tracked, ok := nbt.IntValue(field)
			if !ok {
				return protocol.LodestoneTracker{}, false
			}
			tracker.Tracked = tracked != 0
		default:
			return protocol.LodestoneTracker{}, false
		}
	}
	return tracker, true
}

// lodestoneTargetOf reads one target; GlobalPos insists on both the
// dimension identifier and the three-int block.
// vanilla: GlobalPos.MAP_CODEC, BlockPos.CODEC
func lodestoneTargetOf(value nbt.Tag) (*protocol.LodestoneTarget, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return nil, false
	}
	var ints []int32
	dimension, named := "", false
	for name, field := range entry {
		switch name {
		case "dimension":
			text, ok := field.(nbt.String)
			if !ok || !protocol.ValidIdentifier(string(text)) {
				return nil, false
			}
			dimension, named = string(text), true
		case "pos":
			values, ok := intArrayOf3(field)
			if !ok {
				return nil, false
			}
			ints = values
		default:
			return nil, false
		}
	}
	if !named || ints == nil {
		return nil, false
	}
	return &protocol.LodestoneTarget{Dimension: dimension, X: ints[0], Y: ints[1], Z: ints[2]}, true
}

// intArrayOf3 reads a BlockPos as an int array or, as NbtOps also offers,
// a list of three numbers.
// vanilla: BlockPos.CODEC, NbtOps.getIntStream
func intArrayOf3(value nbt.Tag) ([]int32, bool) {
	switch v := value.(type) {
	case nbt.IntArray:
		if len(v) != 3 {
			return nil, false
		}
		return v, true
	case nbt.List:
		if len(v.Items) != 3 {
			return nil, false
		}
		ints := make([]int32, 3)
		for i, item := range v.Items {
			n, ok := nbt.IntValue(item)
			if !ok {
				return nil, false
			}
			ints[i] = n
		}
		return ints, true
	default:
		return nil, false
	}
}

// potionEffectsOf reads a custom_effects list; the show flags default to
// true, as the codec reads their absence. An empty list of any element
// type is accepted (NbtOps.getStream).
// vanilla: MobEffectInstance.CODEC
func potionEffectsOf(value nbt.Tag) ([]protocol.PotionEffectSpec, bool) {
	list, ok := value.(nbt.List)
	if !ok || (list.Elem != nbt.TypeCompound && len(list.Items) > 0) {
		return nil, false
	}
	specs := make([]protocol.PotionEffectSpec, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		spec, ok := potionEffectOf(entry)
		if !ok {
			return nil, false
		}
		specs = append(specs, spec)
	}
	return specs, true
}

// potionEffectOf reads one custom effect; false for a missing name, an
// effect this version lacks or a field this codec does not write.
// vanilla: MobEffectInstance.CODEC
func potionEffectOf(entry nbt.Compound) (protocol.PotionEffectSpec, bool) {
	name, ok := nbt.Get[nbt.String](entry, "id")
	if !ok {
		return protocol.PotionEffectSpec{}, false
	}
	id, ok := MobEffectID(string(name))
	if !ok {
		return protocol.PotionEffectSpec{}, false
	}
	return potionEffectDetails(entry, id)
}

// potionEffectDetails reads an effect's details, which a hidden effect
// repeats without a name, wearing its parent's id. show_particles defaults
// to true and show_icon to show_particles.
// vanilla: MobEffectInstance.Details.MAP_CODEC
func potionEffectDetails(entry nbt.Compound, id int32) (protocol.PotionEffectSpec, bool) {
	spec := protocol.PotionEffectSpec{ID: id, ShowParticles: true}
	icon := false
	for name, field := range entry {
		switch name {
		case "id":
			// A name inside a hidden effect is not part of the details
			// codec; the caller's id counts.
		case "amplifier":
			n, ok := unsignedByteOf(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.Amplifier = int32(n)
		case "duration":
			n, ok := nbt.IntValue(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.Duration = n
		case "ambient":
			b, ok := boolValue(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.Ambient = b
		case "show_particles":
			b, ok := boolValue(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.ShowParticles = b
		case "show_icon":
			b, ok := boolValue(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.ShowIcon = b
			icon = true
		case "hidden_effect":
			hiddenEntry, ok := nbt.CompoundView(field)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			hidden, ok := potionEffectDetails(hiddenEntry, id)
			if !ok {
				return protocol.PotionEffectSpec{}, false
			}
			spec.Hidden = &hidden
		default:
			return protocol.PotionEffectSpec{}, false
		}
	}
	if !icon {
		spec.ShowIcon = spec.ShowParticles
	}
	return spec, true
}

// unsignedByteOf reads a byte as its unsigned value and any other numeric
// only within 0..255.
// vanilla: ExtraCodecs.UNSIGNED_BYTE
func unsignedByteOf(field nbt.Tag) (uint8, bool) {
	if b, ok := field.(nbt.Byte); ok {
		return uint8(b), true
	}
	n, ok := nbt.IntValue(field)
	if !ok || n < 0 || n > 255 {
		return 0, false
	}
	return uint8(n), true
}

// enchantmentsOf reads an enchantments component, registry name to level;
// an unknown name or a level the codec refuses refuses the component.
func enchantmentsOf(value nbt.Tag) (map[int32]int32, bool) {
	entries, ok := nbt.CompoundView(value)
	if !ok {
		return nil, false
	}
	if len(entries) == 0 {
		// An empty component is the default and reads as absent.
		return nil, true
	}
	levels := make(map[int32]int32, len(entries))
	for name, level := range entries {
		id, ok := EnchantmentID(string(name))
		if !ok {
			return nil, false
		}
		n, ok := nbt.IntValue(level)
		if !ok || n < 1 || n > 255 {
			return nil, false
		}
		levels[id] = int32(n)
	}
	return levels, true
}

// BlockStatePropertiesTag reads a block_state component, property name to
// string value. Names are not checked (the block's placement decides), but a
// non-string value refuses the component.
// vanilla: BlockItemStateProperties.CODEC
func BlockStatePropertiesTag(value nbt.Tag) (map[string]string, bool) {
	entries, ok := nbt.CompoundView(value)
	if !ok {
		return nil, false
	}
	if len(entries) == 0 {
		// An empty component is its own default, and reads as absent.
		return nil, true
	}
	props := make(map[string]string, len(entries))
	for name, field := range entries {
		text, ok := field.(nbt.String)
		if !ok {
			return nil, false
		}
		props[name] = string(text)
	}
	return props, true
}

// StoredStackBase reads a stored stack's item and count alone, so a caller
// keeping the whole compound (a jukebox's disc, ejected even with an
// unshowable component) still knows the item. False for no stack this
// version has.
func StoredStackBase(entry nbt.Compound) (protocol.ItemStack, bool) {
	return storedStackSans(entry)
}

// storedStackSans reads a stored item's id (namespace defaulted) and count
// (default 1, including partial results outside 1..99), ignoring components;
// air is the empty stack. Numeric counts are narrowed: `count:5b` is five;
// non-numeric counts are rejected.
// vanilla: ItemStack.MAP_CODEC, TagValueInput.read
func storedStackSans(entry nbt.Compound) (protocol.ItemStack, bool) {
	name, ok := nbt.Get[nbt.String](entry, "id")
	if !ok {
		return protocol.ItemStack{}, false
	}
	id, ok := ItemID(Qualify(string(name)))
	if !ok {
		return protocol.ItemStack{}, false
	}
	count := int32(1)
	tag, present := entry["count"]
	if _, legacy := entry["Count"]; legacy && !present {
		// A pre-1.20.5 stack is not this version's; rejecting it keeps its bytes.
		return protocol.ItemStack{}, false
	}
	if present {
		if count, ok = nbt.IntValue(tag); !ok {
			return protocol.ItemStack{}, false
		}
		if count < 1 || count > 99 {
			count = 1
		}
	}
	if Item(id) == ItemAir {
		// Air is empty whatever its count.
		return protocol.ItemStack{}, true
	}
	return protocol.ItemStack{ItemID: id, Count: count}, true
}

// EnchantmentID returns an enchantment's ID in minecraft:enchantment, which
// the wire names it by; a name this version lacks reads as absent.
func EnchantmentID(name string) (int32, bool) {
	r, ok := Get("minecraft:enchantment")
	if !ok {
		return -1, false
	}
	if id := r.ID(Qualify(name)); id >= 0 {
		return id, true
	}
	return -1, false
}

// EnchantmentName returns the name an enchantment ID travels under in a
// file, or false for an ID the registry lacks.
func EnchantmentName(id int32) (string, bool) {
	r, ok := Get("minecraft:enchantment")
	if !ok || id < 0 || int(id) >= len(r.Entries) {
		return "", false
	}
	return r.Entries[id], true
}

// The hooks let protocol's hashed slot checksums and block predicates
// resolve enchantment, item and block-tag names: protocol has the hashing
// and codec, this package the names, and neither imports the other further.
func init() {
	protocol.EnchantmentName = EnchantmentName
	protocol.ItemName = ItemName
	protocol.ItemOminousBottleAmplifier = func(item int32) (int32, bool) {
		return 0, Item(item) == ItemOminousBottle
	}
	protocol.BlockTagKnown = BlockTagKnown
	protocol.DamageTypeTagIndex = func(name string) uint16 { return uint16(DamageTypeTagOf(name)) }
	protocol.RegistryLen = func(name string) (int, bool) {
		r, ok := Get(name)
		return len(r.Entries), ok
	}
}

// dyeColorIDs numbers the sixteen DyeColor names in enum order, which a
// banner layer's dye ID indexes.
var dyeColorIDs = func() map[string]int32 {
	ids := make(map[string]int32, len(DyeNames))
	for id, name := range DyeNames {
		ids[name] = int32(id)
	}
	return ids
}()

// BannerPatternsTag writes a banner_patterns list in application order: a
// registry pattern as its name, an inline one as its asset and translation
// key, each with a dye name. An out-of-range pattern or dye is left out.
// vanilla: BannerPatternLayers.CODEC, BannerPattern.CODEC
func BannerPatternsTag(layers []protocol.BannerPatternLayer) nbt.List {
	reg, _ := Get("minecraft:banner_pattern")
	items := make([]nbt.Tag, 0, len(layers))
	for _, layer := range layers {
		if layer.Dye < 0 || int(layer.Dye) >= len(DyeNames) {
			continue
		}
		var pattern nbt.Tag
		switch {
		case layer.Pattern < 0:
			inline := &nbt.OrderedCompound{}
			inline.Set("asset_id", nbt.String(layer.AssetID))
			inline.Set("translation_key", nbt.String(layer.TranslationKey))
			pattern = inline
		case int(layer.Pattern) < len(reg.Entries):
			pattern = nbt.String(reg.Entries[layer.Pattern])
		default:
			continue
		}
		entry := &nbt.OrderedCompound{}
		entry.Set("pattern", pattern)
		entry.Set("color", nbt.String(DyeNames[layer.Dye]))
		items = append(items, entry)
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// BannerPatternsOf reads a banner_patterns list as the layers it holds,
// reporting false for an entry naming a pattern or dye this version lacks.
// vanilla: BannerPatternLayers.CODEC, BannerPattern.CODEC
func BannerPatternsOf(patterns nbt.List) ([]protocol.BannerPatternLayer, bool) {
	reg, ok := Get("minecraft:banner_pattern")
	if !ok {
		return nil, false
	}
	layers := make([]protocol.BannerPatternLayer, 0, len(patterns.Items))
	for _, item := range patterns.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		colorName, ok := nbt.Get[nbt.String](entry, "color")
		if !ok {
			return nil, false
		}
		dye, ok := dyeColorIDs[string(colorName)]
		if !ok {
			return nil, false
		}
		layer := protocol.BannerPatternLayer{Dye: dye}
		switch pattern := entry["pattern"].(type) {
		case nbt.String:
			// vanilla: Identifier.CODEC reads a bare path in the minecraft
			// namespace.
			layer.Pattern = reg.ID(Qualify(string(pattern)))
			if layer.Pattern < 0 {
				return nil, false
			}
		case nbt.CompoundTag:
			asset, ok := nbt.Get[nbt.String](pattern.Entries(), "asset_id")
			if !ok {
				return nil, false
			}
			key, ok := nbt.Get[nbt.String](pattern.Entries(), "translation_key")
			if !ok {
				return nil, false
			}
			layer.Pattern = -1
			layer.AssetID = string(asset)
			layer.TranslationKey = string(key)
		default:
			return nil, false
		}
		layers = append(layers, layer)
	}
	return layers, true
}

// potSides are the keys of a pot's four sides, in the order the component
// lists them.
var potSides = [4]string{"back", "left", "right", "front"}

// PotAllBrick is the decorated pot item's default minecraft:pot_decorations,
// a brick on every side.
// vanilla: PotDecorations.allBrick, Items.DECORATED_POT
func PotAllBrick() [4]protocol.ItemStack {
	brick := protocol.ItemStack{ItemID: int32(ItemBrick), Count: 1}
	return [4]protocol.ItemStack{brick, brick, brick, brick}
}

// IsPotAllBrick reports whether sides are PotAllBrick.
func IsPotAllBrick(sides [4]protocol.ItemStack) bool {
	brick := PotAllBrick()
	for i := range sides {
		if !sides[i].Equal(brick[i]) {
			return false
		}
	}
	return true
}

// PotDecorationsTag writes the sides' item templates under their names; an
// absent (empty) side is left out.
// vanilla: PotDecorations.CODEC
func PotDecorationsTag(sides [4]protocol.ItemStack) *nbt.OrderedCompound {
	tag := &nbt.OrderedCompound{}
	for i, side := range sides {
		if !side.Empty() {
			tag.Set(potSides[i], templateTag(side))
		}
	}
	return tag
}

// PotDecorationsOf reads the four optional item templates of a pot, an empty
// stack for an absent side; false for a side that is not a stored stack.
// vanilla: PotDecorations.CODEC
func PotDecorationsOf(tag nbt.Compound) ([4]protocol.ItemStack, bool) {
	var sides [4]protocol.ItemStack
	for i, key := range potSides {
		value, present := tag[key]
		if !present {
			continue
		}
		entry, ok := nbt.CompoundView(value)
		if !ok {
			return sides, false
		}
		side, ok := StoredStack(entry)
		if !ok {
			return sides, false
		}
		sides[i] = side
	}
	return sides, true
}

// BeesTag writes a bees list: each occupant's network NBT with its entity
// type restored as "id", and the two tick counts. An unknown entity type
// is left out.
// vanilla: BeehiveBlockEntity.Occupant.CODEC, TypedEntityData.of
func BeesTag(bees []protocol.Bee) nbt.List {
	items := make([]nbt.Tag, 0, len(bees))
	for _, bee := range bees {
		if bee.EntityType < 0 || int(bee.EntityType) >= len(EntityTypes) {
			continue
		}
		data := typedEntityDataTag(EntityTypes[bee.EntityType], bee.Data)
		entry := &nbt.OrderedCompound{}
		entry.Set("entity_data", data)
		entry.Set("ticks_in_hive", nbt.Int(bee.TicksInHive))
		entry.Set("min_ticks_in_hive", nbt.Int(bee.MinTicksInHive))
		items = append(items, entry)
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// BeesOf reads a bees list as the occupants a bees component holds, dropping
// each occupant's "id". A missing tick count reads as zero.
// vanilla: BeehiveBlockEntity.Occupant.CODEC, TypedEntityData.of
func BeesOf(list nbt.List) ([]protocol.Bee, bool) {
	out := make([]protocol.Bee, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		entityType, stripped, ok := typedEntityDataOf(entry["entity_data"], EntityTypeID)
		if !ok {
			return nil, false
		}
		ticks, _ := nbt.IntValue(entry["ticks_in_hive"])
		minTicks, _ := nbt.IntValue(entry["min_ticks_in_hive"])
		out = append(out, protocol.Bee{
			EntityType:     entityType,
			Data:           stripped,
			TicksInHive:    ticks,
			MinTicksInHive: minTicks,
		})
	}
	return out, true
}

// stewEffectsOf reads SuspiciousStewEffects.CODEC; durations default to 160 ticks.
func stewEffectsOf(value nbt.Tag) ([]protocol.StewEffect, bool) {
	list, ok := value.(nbt.List)
	if !ok {
		return nil, false
	}
	effects := make([]protocol.StewEffect, 0, len(list.Items))
	for _, v := range list.Items {
		e, ok := nbt.CompoundView(v)
		if !ok {
			return nil, false
		}
		name, ok := nbt.Get[nbt.String](e, "id")
		if !ok {
			return nil, false
		}
		id, ok := MobEffectID(Qualify(string(name)))
		if !ok {
			return nil, false
		}
		duration, ok := nbt.IntValue(e["duration"])
		if !ok {
			duration = 160
		}
		effects = append(effects, protocol.StewEffect{Effect: id, Duration: duration})
	}
	return effects, true
}

// dyedColorOf reads DyedItemColor.CODEC's integer or three float channels.
func dyedColorOf(value nbt.Tag) (int32, bool) {
	if rgb, ok := nbt.IntValue(value); ok {
		return rgb, true
	}
	list, ok := value.(nbt.List)
	if !ok || len(list.Items) != 3 {
		return 0, false
	}
	var rgb int32
	for _, v := range list.Items {
		f, ok := nbt.FloatField(nbt.Compound{"v": v}, "v")
		if !ok {
			return 0, false
		}
		// vanilla: ARGB.as8BitChannel
		channel, _ := nbt.IntElement(nbt.Float(f * 255))
		rgb = rgb<<8 | channel
	}
	return rgb | -16777216, true
}

// tagElem is the element type a list of tags is written with: a list of
// mixed types (text lines can be strings or compounds) is a list of compounds.
func tagElem(tags []nbt.Tag) nbt.Type {
	if len(tags) == 0 {
		return nbt.TypeEnd
	}
	elem := tags[0].Type()
	for _, t := range tags[1:] {
		if t.Type() != elem {
			return nbt.TypeCompound
		}
	}
	return elem
}

// attributeModifiersTag writes the entries as a list of entry compounds, the
// slot and display only away from their defaults; false when an entry names
// an attribute, operation or slot group this version lacks.
// vanilla: ItemAttributeModifiers.CODEC
func attributeModifiersTag(entries []protocol.AttributeModifierEntry) (nbt.List, bool) {
	items := make([]nbt.Tag, 0, len(entries))
	for _, e := range entries {
		if e.Operation < 0 || int(e.Operation) >= len(attributeOperationNames) ||
			e.Slot < 0 || int(e.Slot) >= len(slotGroupNames) {
			return nbt.List{}, false
		}
		name := ""
		for _, a := range Attributes {
			if a.ID == e.Attribute {
				name = a.Name
				break
			}
		}
		if name == "" {
			return nbt.List{}, false
		}
		entry := &nbt.OrderedCompound{}
		entry.Set("type", nbt.String(name))
		entry.Set("id", nbt.String(e.ID))
		entry.Set("amount", nbt.Double(e.Amount))
		entry.Set("operation", nbt.String(attributeOperationNames[e.Operation]))
		if e.Slot != 0 {
			entry.Set("slot", nbt.String(slotGroupNames[e.Slot]))
		}
		if e.Display == 1 {
			display := &nbt.OrderedCompound{}
			display.Set("type", nbt.String("hidden"))
			entry.Set("display", display)
		}
		items = append(items, entry)
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}, true
}

// attributeModifiersOf reads attributeModifiersTag's shape; override text
// is not held, so a display of that type refuses the component.
func attributeModifiersOf(value nbt.Tag) ([]protocol.AttributeModifierEntry, bool) {
	list, ok := value.(nbt.List)
	if !ok {
		return nil, false
	}
	entries := make([]protocol.AttributeModifierEntry, 0, len(list.Items))
	for _, item := range list.Items {
		tag, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		typeName, _ := nbt.Get[nbt.String](tag, "type")
		attr, ok := AttributeByName(Qualify(string(typeName)))
		if !ok {
			return nil, false
		}
		id, ok := nbt.Get[nbt.String](tag, "id")
		if !ok {
			return nil, false
		}
		// vanilla: AttributeModifier.MAP_CODEC (Codec.DOUBLE accepts every numeric tag).
		amount, ok := nbt.NumberValue(tag["amount"])
		if !ok {
			return nil, false
		}
		opName, _ := nbt.Get[nbt.String](tag, "operation")
		op, ok := AttributeOperationByName(string(opName))
		if !ok {
			return nil, false
		}
		e := protocol.AttributeModifierEntry{Attribute: attr.ID, ID: string(id), Amount: amount, Operation: int32(op)}
		if slot, has := nbt.Get[nbt.String](tag, "slot"); has {
			if e.Slot, ok = SlotGroupID(string(slot)); !ok {
				return nil, false
			}
		}
		if display, has := tag["display"]; has {
			d, isCompound := nbt.CompoundView(display)
			kind, _ := nbt.Get[nbt.String](d, "type")
			switch {
			case !isCompound:
				return nil, false
			case kind == "default":
			case kind == "hidden":
				e.Display = 1
			default:
				return nil, false
			}
		}
		entries = append(entries, e)
	}
	return entries, true
}
