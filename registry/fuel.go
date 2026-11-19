package registry

// FuelBurnTicks is how many ticks one of the item burns for in a furnace,
// 0 for an item that is not fuel. vanilla: FuelValues.burnDuration
func FuelBurnTicks(id int32) int32 {
	if id < 0 || id >= ItemCount {
		return 0
	}
	return fuelBurnTicks[id]
}

// BrewingFuelUses is how many brews one of the item loads a brewing stand
// with, 0 for an item that is not brewing fuel. vanilla: BrewingFuel.uses
func BrewingFuelUses(id int32) int32 {
	if id < 0 || id >= ItemCount {
		return 0
	}
	return brewingFuelUses[id]
}
