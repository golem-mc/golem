package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// useAnimationNames is ItemUseAnimation by wire ID.
// vanilla: ItemUseAnimation
var useAnimationNames = [...]string{
	"none", "eat", "drink", "block", "bow", "trident", "crossbow", "spyglass", "toot_horn", "brush", "bundle", "spear",
}

// consumeEffectNames is ConsumeEffect.Type by wire ID.
var consumeEffectNames = [...]string{
	protocol.ConsumeEffectApplyEffects:     "minecraft:apply_effects",
	protocol.ConsumeEffectRemoveEffects:    "minecraft:remove_effects",
	protocol.ConsumeEffectClearAllEffects:  "minecraft:clear_all_effects",
	protocol.ConsumeEffectTeleportRandomly: "minecraft:teleport_randomly",
	protocol.ConsumeEffectPlaySound:        "minecraft:play_sound",
}

// defaultConsumeSeconds is Consumable.DEFAULT_CONSUME_SECONDS.
const defaultConsumeSeconds = 1.6

// StackConsumable returns how the stack is consumed: its own consumable
// component, else its item's default. A stack with a food of its own but no
// consumable anywhere, or whose patch removes it, is not consumable.
// vanilla: Item.use, stack.get(DataComponents.CONSUMABLE)
func StackConsumable(stack protocol.ItemStack) (Consumable, bool) {
	if stack.Removes(protocol.DataComponentConsumable) {
		return Consumable{}, false
	}
	own, ok := stack.ConsumableOf()
	remainder, hasRemainder := stack.UseRemainderOf()
	removedRemainder := stack.Removes(protocol.DataComponentUseRemainder)
	if !ok && !hasRemainder && !removedRemainder {
		return ConsumableOf(stack.ItemID)
	}
	var c Consumable
	if ok {
		c = Consumable{
			Seconds:   own.Seconds,
			Sound:     own.Sound,
			Drink:     own.Animation == protocol.ItemUseAnimationDrink,
			Remainder: ConsumableRemainderOf(stack.ItemID),
		}
		for _, e := range own.Effects {
			c.Effects = append(c.Effects, consumeEffectOf(e))
		}
	} else {
		if c, ok = ConsumableOf(stack.ItemID); !ok {
			return Consumable{}, false
		}
	}
	if removedRemainder {
		c.Remainder = protocol.ItemStack{}
	} else if hasRemainder {
		// vanilla: ItemStackTemplate.create, ItemStack.validateStrict
		if remainder.Count > StackMaxStackSize(remainder) {
			remainder = protocol.ItemStack{}
		}
		c.Remainder = remainder
	}
	return c, true
}

// StackFood returns what a meal of the stack restores: its own food
// component, else its item's default. False for a stack whose patch
// removes it: such a meal skips the hunger check and restores nothing.
// vanilla: Consumable.canConsume, stack.get(DataComponents.FOOD)
func StackFood(stack protocol.ItemStack) (Food, bool) {
	if stack.Removes(protocol.DataComponentFood) {
		return Food{}, false
	}
	own, ok := stack.FoodOf()
	if !ok {
		return FoodOf(stack.ItemID)
	}
	return Food{Nutrition: own.Nutrition, Saturation: own.Saturation, CanAlwaysEat: own.CanAlwaysEat}, true
}

// ConsumableRemainderOf is the use_remainder item an item's default
// consumable leaves behind, empty for none.
func ConsumableRemainderOf(item int32) protocol.ItemStack {
	if c := propsOf(item).Consumable; c != nil {
		return c.Remainder
	}
	return protocol.ItemStack{}
}

func consumeEffectOf(e protocol.ConsumeEffectComponent) ConsumeEffect {
	out := ConsumeEffect{Type: ConsumeEffectType(e.Type)}
	switch e.Type {
	case protocol.ConsumeEffectApplyEffects:
		out.Probability = e.Probability
		for _, spec := range e.Effects {
			out.Effects = append(out.Effects, EffectSpec{
				Effect: MobEffectName(spec.ID), Amplifier: spec.Amplifier, Duration: spec.Duration,
				Ambient: spec.Ambient, ShowParticles: spec.ShowParticles, ShowIcon: spec.ShowIcon,
			})
		}
	case protocol.ConsumeEffectRemoveEffects:
		for _, id := range e.Remove {
			out.Remove = append(out.Remove, MobEffectName(id))
		}
	case protocol.ConsumeEffectTeleportRandomly:
		out.Diameter, out.Directional = e.Diameter, e.Directional
	case protocol.ConsumeEffectPlaySound:
		out.Sound = e.Sound
	}
	return out
}

// foodOf reads a minecraft:food value: nutrition and saturation are
// required.
// vanilla: FoodProperties.DIRECT_CODEC
func foodOf(value nbt.Tag) (protocol.FoodComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.FoodComponent{}, false
	}
	nutrition, ok := nbt.IntValue(entry["nutrition"])
	if !ok || nutrition < 0 {
		return protocol.FoodComponent{}, false
	}
	saturation, ok := floatOf(entry["saturation"])
	if !ok {
		return protocol.FoodComponent{}, false
	}
	food := protocol.FoodComponent{Nutrition: nutrition, Saturation: saturation}
	if field, has := entry["can_always_eat"]; has {
		if food.CanAlwaysEat, ok = boolValue(field); !ok {
			return protocol.FoodComponent{}, false
		}
	}
	return food, true
}

func foodTag(food protocol.FoodComponent) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(2)
	out.Set("nutrition", nbt.Int(food.Nutrition))
	out.Set("saturation", nbt.Float(food.Saturation))
	if food.CanAlwaysEat {
		out.Set("can_always_eat", nbt.Byte(1))
	}
	return out
}

// consumableOf reads a minecraft:consumable value; every field is optional.
// vanilla: Consumable.CODEC
func consumableOf(value nbt.Tag) (protocol.ConsumableComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.ConsumableComponent{}, false
	}
	sound, _ := soundHolderOf(nbt.String(defaultConsumeSound))
	c := protocol.ConsumableComponent{Seconds: defaultConsumeSeconds, Animation: 1, Sound: sound, HasParticles: true}
	for name, field := range entry {
		switch name {
		case "consume_seconds":
			c.Seconds, ok = floatOf(field)
			ok = ok && c.Seconds >= 0
		case "animation":
			c.Animation, ok = nameIndex(useAnimationNames[:], field)
		case "sound":
			c.Sound, ok = soundHolderOf(field)
		case "has_consume_particles":
			c.HasParticles, ok = boolValue(field)
		case "on_consume_effects":
			c.Effects, ok = consumeEffectsOf(field)
		default:
			// Unknown keys are ignored, as the codec ignores them.
			ok = true
		}
		if !ok {
			return protocol.ConsumableComponent{}, false
		}
	}
	return c, true
}

// nameIndex is the index of the string tag's value in names, false for any
// other tag or name.
func nameIndex(names []string, value nbt.Tag) (int32, bool) {
	s, ok := value.(nbt.String)
	if !ok {
		return 0, false
	}
	for i, name := range names {
		if name == string(s) {
			return int32(i), true
		}
	}
	return 0, false
}

func consumeEffectsOf(value nbt.Tag) ([]protocol.ConsumeEffectComponent, bool) {
	list, ok := value.(nbt.List)
	if !ok {
		return nil, false
	}
	out := make([]protocol.ConsumeEffectComponent, 0, len(list.Items))
	for _, item := range list.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		e, ok := consumeEffectComponentOf(entry)
		if !ok {
			return nil, false
		}
		out = append(out, e)
	}
	return out, true
}

// consumeEffectComponentOf reads one on_consume_effects entry.
// vanilla: ConsumeEffect.CODEC and the effects' MapCodecs
func consumeEffectComponentOf(entry nbt.Compound) (protocol.ConsumeEffectComponent, bool) {
	typeName, ok := entry["type"].(nbt.String)
	if !ok {
		return protocol.ConsumeEffectComponent{}, false
	}
	var e protocol.ConsumeEffectComponent
	e.Type, ok = nameIndex(consumeEffectNames[:], nbt.String(Qualify(string(typeName))))
	if !ok {
		return e, false
	}
	switch e.Type {
	case protocol.ConsumeEffectApplyEffects:
		e.Probability = 1
		if e.Effects, ok = potionEffectsOf(entry["effects"]); !ok {
			return e, false
		}
		if field, has := entry["probability"]; has {
			if e.Probability, ok = floatOf(field); !ok || e.Probability < 0 || e.Probability > 1 {
				return e, false
			}
		}
	case protocol.ConsumeEffectRemoveEffects:
		names := []nbt.Tag{entry["effects"]}
		if list, isList := entry["effects"].(nbt.List); isList {
			names = list.Items
		}
		e.Remove = make([]int32, 0, len(names))
		for _, n := range names {
			s, isString := n.(nbt.String)
			id, known := MobEffectID(Qualify(string(s)))
			if !isString || !known {
				return e, false
			}
			e.Remove = append(e.Remove, id)
		}
	case protocol.ConsumeEffectTeleportRandomly:
		e.Diameter, e.Directional = 16, true
		if field, has := entry["diameter"]; has {
			if e.Diameter, ok = floatOf(field); !ok || !(e.Diameter > 0) {
				return e, false
			}
		}
		if field, has := entry["directional_particles"]; has {
			if e.Directional, ok = boolValue(field); !ok {
				return e, false
			}
		}
	case protocol.ConsumeEffectPlaySound:
		if e.Sound, ok = soundHolderOf(entry["sound"]); !ok {
			return e, false
		}
	}
	return e, true
}

// consumableTag writes a minecraft:consumable in the shape consumableOf
// reads, leaving the fields that hold their defaults out.
func consumableTag(c protocol.ConsumableComponent) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(0)
	if c.Seconds != defaultConsumeSeconds {
		out.Set("consume_seconds", nbt.Float(c.Seconds))
	}
	if c.Animation != 1 {
		out.Set("animation", nbt.String(useAnimationNames[c.Animation]))
	}
	putSoundTag(out, "sound", c.Sound, defaultConsumeSound)
	if !c.HasParticles {
		out.Set("has_consume_particles", nbt.Byte(0))
	}
	if len(c.Effects) > 0 {
		list := nbt.List{Elem: nbt.TypeCompound, Items: make([]nbt.Tag, 0, len(c.Effects))}
		for _, e := range c.Effects {
			list.Items = append(list.Items, consumeEffectTag(e))
		}
		out.Set("on_consume_effects", list)
	}
	return out
}

func consumeEffectTag(e protocol.ConsumeEffectComponent) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(1)
	out.Set("type", nbt.String(consumeEffectNames[e.Type]))
	switch e.Type {
	case protocol.ConsumeEffectApplyEffects:
		effects, _ := potionEffectsTag(e.Effects)
		out.Set("effects", effects)
		if e.Probability != 1 {
			out.Set("probability", nbt.Float(e.Probability))
		}
	case protocol.ConsumeEffectRemoveEffects:
		names := nbt.List{Elem: nbt.TypeString, Items: make([]nbt.Tag, 0, len(e.Remove))}
		for _, id := range e.Remove {
			names.Items = append(names.Items, nbt.String(MobEffectName(id)))
		}
		out.Set("effects", names)
	case protocol.ConsumeEffectTeleportRandomly:
		if e.Diameter != 16 {
			out.Set("diameter", nbt.Float(e.Diameter))
		}
		if !e.Directional {
			out.Set("directional_particles", nbt.Byte(0))
		}
	case protocol.ConsumeEffectPlaySound:
		if sound, ok := soundHolderTag(e.Sound); ok {
			out.Set("sound", sound)
		}
	}
	return out
}

// withConsumableTags adds the stack's own consumption components to the
// components written for it.
func withConsumableTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	food, hasFood := stack.FoodOf()
	consumable, hasConsumable := stack.ConsumableOf()
	cooldown, hasCooldown := stack.UseCooldownOf()
	remainder, hasRemainder := stack.UseRemainderOf()
	if (hasFood || hasConsumable || hasCooldown || hasRemainder) && components == nil {
		components = nbt.Compound{}
	}
	if hasRemainder {
		components["minecraft:use_remainder"] = templateTag(remainder)
	}
	if hasCooldown {
		components["minecraft:use_cooldown"] = useCooldownTag(cooldown)
	}
	if hasFood {
		components["minecraft:food"] = foodTag(food)
	}
	if hasConsumable {
		components["minecraft:consumable"] = consumableTag(consumable)
	}
	return components
}

// readConsumableComponent lays the named consumption component on the
// stack; false for a value the codec refuses, modeled false for any other
// name, which stays one of the file's own.
func readConsumableComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:cooking_fuel":
		return readFuelComponent(stack, value, "burn_time", (*protocol.ItemStack).SetCookingFuel), true
	case "minecraft:brewing_fuel":
		return readFuelComponent(stack, value, "uses", (*protocol.ItemStack).SetBrewingFuel), true
	case "minecraft:compostable":
		return readCompostableComponent(stack, value), true
	case "minecraft:max_stack_size":
		return readMaxStackSizeComponent(stack, value), true
	case "minecraft:use_remainder":
		remainder, ok := useRemainderOf(value)
		switch {
		case !ok:
		case remainder.Equal(ConsumableRemainderOf(stack.ItemID)):
			stack.ResetComponent(protocol.DataComponentUseRemainder)
		default:
			stack.SetUseRemainder(remainder)
		}
		return ok, true
	case "minecraft:food":
		food, ok := foodOf(value)
		if ok {
			stack.SetFood(food)
		}
		return ok, true
	case "minecraft:consumable":
		consumable, ok := consumableOf(value)
		if ok {
			stack.SetConsumable(consumable)
		}
		return ok, true
	case "minecraft:use_cooldown":
		cooldown, ok := useCooldownOf(value)
		if ok {
			stack.SetUseCooldown(cooldown)
		}
		return ok, true
	}
	return false, false
}

// vanilla: ItemStackTemplate.CODEC
func useRemainderOf(value nbt.Tag) (protocol.ItemStack, bool) {
	if name, ok := value.(nbt.String); ok {
		value = nbt.Compound{"id": name}
	}
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.ItemStack{}, false
	}
	if components, has := entry["components"]; has {
		if _, ok := nbt.CompoundView(components); !ok {
			return protocol.ItemStack{}, false
		}
	}
	if tag, has := entry["count"]; has {
		// A template's explicit count must be 1..99; a stored stack's is recovered instead.
		if count, ok := nbt.IntValue(tag); !ok || count < 1 || count > 99 {
			return protocol.ItemStack{}, false
		}
	}
	remainder, ok := StoredStack(entry)
	return remainder, ok && Item(remainder.ItemID) != ItemAir && !remainder.Empty()
}

// useCooldownOf reads a minecraft:use_cooldown value: a positive seconds
// and an optional identifier group.
// vanilla: UseCooldown.CODEC
func useCooldownOf(value nbt.Tag) (protocol.UseCooldownComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.UseCooldownComponent{}, false
	}
	var c protocol.UseCooldownComponent
	field, has := entry["seconds"]
	if !has {
		return c, false
	}
	if c.Seconds, ok = floatOf(field); !ok || !(c.Seconds > 0) {
		return c, false
	}
	if field, has := entry["cooldown_group"]; has {
		id, ok := field.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(id)) {
			return c, false
		}
		c.Group = Qualify(string(id))
	}
	return c, true
}

// useCooldownTag is the saved form of a use_cooldown, the group left out
// when it is the item's own.
func useCooldownTag(c protocol.UseCooldownComponent) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(1)
	out.Set("seconds", nbt.Float(c.Seconds))
	if c.Group != "" {
		out.Set("cooldown_group", nbt.String(c.Group))
	}
	return out
}

// itemUseCooldownSeconds is the use_cooldown an item has by default.
// vanilla: Items.ENDER_PEARL, WIND_CHARGE, CHORUS_FRUIT (Item.Properties.useCooldown)
func itemUseCooldownSeconds(item Item) float32 {
	switch item {
	case ItemEnderPearl, ItemChorusFruit:
		return 1.0
	case ItemWindCharge:
		return 0.5
	}
	return 0
}

// CooldownGroup is the cooldown group a use of the stack starts and checks:
// its use_cooldown's group, else the item's registry name.
// vanilla: ItemCooldowns.getCooldownGroup
func CooldownGroup(stack protocol.ItemStack) string {
	if !stack.Removes(protocol.DataComponentUseCooldown) {
		if own, ok := stack.UseCooldownOf(); ok && own.Group != "" {
			return own.Group
		}
	}
	name, _ := ItemName(stack.ItemID)
	return name
}

// StackUseCooldown returns the cooldown a finished use of the stack starts:
// its own use_cooldown, else its item's default; false for none.
// vanilla: ItemStack.applyAfterUseComponentSideEffects, UseCooldown.ticks
func StackUseCooldown(stack protocol.ItemStack) (ticks int32, group string, ok bool) {
	if stack.Removes(protocol.DataComponentUseCooldown) {
		return 0, "", false
	}
	seconds := itemUseCooldownSeconds(Item(stack.ItemID))
	if own, has := stack.UseCooldownOf(); has {
		seconds = own.Seconds
	}
	if seconds <= 0 {
		return 0, "", false
	}
	return int32(seconds * 20), CooldownGroup(stack), true
}
