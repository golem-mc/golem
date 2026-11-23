package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// StackCookingFuel returns the stack's own cooking_fuel as burn time and
// speed providers; own is false when it carries none, and the caller then
// falls back to the item default unless removed.
func StackCookingFuel(stack protocol.ItemStack) (burn, speed LootNumber, own, removed bool) {
	return stackFuel(stack, protocol.DataComponentCookingFuel)
}

// HasCookingFuel reports whether the stack has a cooking_fuel component, its
// own or the item's.
// vanilla: AbstractFurnaceMenu.isFuel
func HasCookingFuel(stack protocol.ItemStack) bool {
	_, _, own, removed := StackCookingFuel(stack)
	return !stack.Empty() && (own || !removed && FuelBurnTicks(stack.ItemID) > 0)
}

// HasBrewingFuel reports whether the stack has a brewing_fuel component, its
// own or the item's.
// vanilla: BrewingStandMenu.FuelSlot.mayPlace
func HasBrewingFuel(stack protocol.ItemStack) bool {
	_, _, own, removed := StackBrewingFuel(stack)
	return !stack.Empty() && (own || !removed && BrewingFuelUses(stack.ItemID) > 0)
}

// StackBrewingFuel is StackCookingFuel for brewing_fuel: uses and speed.
func StackBrewingFuel(stack protocol.ItemStack) (uses, speed LootNumber, own, removed bool) {
	return stackFuel(stack, protocol.DataComponentBrewingFuel)
}

func stackFuel(stack protocol.ItemStack, component int32) (count, speed LootNumber, own, removed bool) {
	if stack.Removes(component) {
		return nil, nil, false, true
	}
	if len(stack.Raw) == 0 {
		return nil, nil, false, false
	}
	c, ok := stack.CookingFuelOf()
	if component == protocol.DataComponentBrewingFuel {
		c, ok = stack.BrewingFuelOf()
	}
	if !ok {
		return nil, nil, false, false
	}
	count, speed = LootConstant{Value: float64(c.Count)}, LootConstant{Value: float64(c.Speed)}
	if c.CountProvider != "" {
		count = LootProviderRef{Name: Qualify(c.CountProvider)}
	}
	if c.SpeedProvider != "" {
		speed = LootProviderRef{Name: Qualify(c.SpeedProvider)}
	}
	return count, speed, true, false
}

// readFuelComponent reads cooking_fuel (burnKey "burn_time") or brewing_fuel
// ("uses") from a stored stack.
// vanilla: CookingFuel.CODEC, BrewingFuel.CODEC, ResolvableInt.CODEC
func readFuelComponent(stack *protocol.ItemStack, value nbt.Tag, countKey string, set func(*protocol.ItemStack, protocol.FuelComponent)) bool {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return false
	}
	var c protocol.FuelComponent
	if n, ok := nbt.IntValue(entry[countKey]); ok {
		c.Count = n
	} else if name, ok := entry[countKey].(nbt.String); ok && protocol.ValidIdentifier(string(name)) {
		c.CountProvider = Qualify(string(name))
	} else {
		return false
	}
	if v, ok := nbt.NumberValue(entry["speed_multiplier"]); ok {
		c.Speed = float32(v)
	} else if name, ok := entry["speed_multiplier"].(nbt.String); ok && protocol.ValidIdentifier(string(name)) {
		c.SpeedProvider = Qualify(string(name))
	} else {
		return false
	}
	set(stack, c)
	return true
}

func fuelTag(c protocol.FuelComponent, countKey string) nbt.Tag {
	var count, speed nbt.Tag = nbt.Int(c.Count), nbt.Float(c.Speed)
	if c.CountProvider != "" {
		count = nbt.String(c.CountProvider)
	}
	if c.SpeedProvider != "" {
		speed = nbt.String(c.SpeedProvider)
	}
	tag := nbt.NewOrderedCompound(2)
	tag.Set(countKey, count)
	tag.Set("speed_multiplier", speed)
	return tag
}

func withFuelTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	if c, ok := stack.CookingFuelOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:cooking_fuel"] = fuelTag(c, "burn_time")
	}
	if c, ok := stack.BrewingFuelOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:brewing_fuel"] = fuelTag(c, "uses")
	}
	return components
}
