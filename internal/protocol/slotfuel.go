package protocol

import "strings"

// FuelComponent is a cooking_fuel (Count = burn_time) or brewing_fuel
// (Count = uses) component: a resolvable int and a resolvable float, each an
// inline number or a context provider key. An empty provider selects the
// number.
// vanilla: CookingFuel, BrewingFuel, ResolvableInt, ResolvableFloat
type FuelComponent struct {
	Count         int32
	CountProvider string
	Speed         float32
	SpeedProvider string
}

func readResolvable(r *Reader, number func(), provider *string) bool {
	if r.ReadBool() {
		number()
		return r.Err() == nil
	}
	*provider = r.ReadString(32767)
	if !ValidIdentifier(*provider) {
		return false
	}
	if !strings.ContainsRune(*provider, ':') {
		*provider = "minecraft:" + *provider
	}
	return r.Err() == nil
}

func readFuel(r *Reader) (FuelComponent, bool) {
	var c FuelComponent
	if !readResolvable(r, func() { c.Count = r.ReadI32() }, &c.CountProvider) {
		return c, false
	}
	ok := readResolvable(r, func() { c.Speed = r.ReadF32() }, &c.SpeedProvider)
	return c, ok
}

func (c FuelComponent) raw(component int32) RawComponent {
	var b Buffer
	b.PutBool(c.CountProvider == "")
	if c.CountProvider == "" {
		b.PutI32(c.Count)
	} else {
		b.PutString(c.CountProvider)
	}
	b.PutBool(c.SpeedProvider == "")
	if c.SpeedProvider == "" {
		b.PutF32(c.Speed)
	} else {
		b.PutString(c.SpeedProvider)
	}
	return RawComponent{Type: component, Data: b.Bytes()}
}

func (s ItemStack) fuelOf(component int32) (FuelComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			r := NewReader(raw.Data)
			c, ok := readFuel(r)
			return c, ok && r.Remaining() == 0
		}
	}
	return FuelComponent{}, false
}

// CookingFuelOf returns the stack's own cooking_fuel component.
func (s ItemStack) CookingFuelOf() (FuelComponent, bool) {
	return s.fuelOf(DataComponentCookingFuel)
}

// SetCookingFuel replaces the stack's cooking_fuel component.
func (s *ItemStack) SetCookingFuel(c FuelComponent) { s.putRaw(c.raw(DataComponentCookingFuel)) }

// BrewingFuelOf returns the stack's own brewing_fuel component.
func (s ItemStack) BrewingFuelOf() (FuelComponent, bool) {
	return s.fuelOf(DataComponentBrewingFuel)
}

// SetBrewingFuel replaces the stack's brewing_fuel component.
func (s *ItemStack) SetBrewingFuel(c FuelComponent) { s.putRaw(c.raw(DataComponentBrewingFuel)) }
