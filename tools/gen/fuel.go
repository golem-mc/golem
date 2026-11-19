package main

import "errors"

func init() {
	register(generator{
		name: "fuel",
		doc:  "registry/fuel_gen.go: every item's furnace burn time in ticks, and the brewing fuels' uses",
		run:  runFuel,
	})
}

// The burn times are the cooking_fuel components of the item components report;
// the brewing fuels are the brewing_fuel components.

func runFuel(e *env) error {
	fuels, present := componentFuels(e)
	if !present {
		return errors.New("the item components report has no minecraft:cooking_fuel component")
	}
	return writeFuels(e, fuels, brewingFuels(e))
}

func writeFuels(e *env, fuel, brewing map[string]int) error {
	if err := needAtLeast(len(fuel), 100, "fuels"); err != nil {
		return err
	}

	f := e.file("registry", "by tools/gen fuel from a vanilla {version} server")
	f.WriteString(`// fuelBurnTicks is FuelValues.vanillaBurnTimes, by item ID: how many
// ticks one of the item burns for in a furnace.
var fuelBurnTicks = [ItemCount]int32{
`)
	for _, name := range sortedKeys(fuel) {
		id, err := ident("Item", name)
		if err != nil {
			return err
		}
		f.Printf("\t%s: %d,\n", id, fuel[name])
	}
	f.WriteString(`}

// brewingFuelUses is how many brews one of the item loads a brewing stand
// with, by item ID.
var brewingFuelUses = [ItemCount]int32{
`)
	for _, name := range sortedKeys(brewing) {
		id, err := ident("Item", name)
		if err != nil {
			return err
		}
		f.Printf("\t%s: %d,\n", id, brewing[name])
	}
	f.WriteString("}\n")
	e.logf("wrote %d fuels, %d brewing fuels", len(fuel), len(brewing))
	return e.emit("registry/fuel_gen.go", f)
}
