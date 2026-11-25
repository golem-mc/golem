package protocol

import "slices"

// The minecraft:charged_projectiles component is held as the bytes it arrived
// in, like the bundle's, whose codec it shares: a list of item templates.

// ChargedProjectiles returns the projectiles a crossbow stack is loaded with;
// nil for an unloaded one.
func (s ItemStack) ChargedProjectiles() []ItemStack {
	items, _ := s.ChargedProjectilesOf()
	return items
}

// ChargedProjectilesOf returns the patched minecraft:charged_projectiles items
// and whether the component is explicitly patched on the stack.
func (s ItemStack) ChargedProjectilesOf() ([]ItemStack, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentChargedProjectiles {
			items, ok := bundleItemsOf(raw.Data)
			if !ok {
				return nil, false
			}
			if len(s.intangibleLoaded) == len(items) {
				for i := range items {
					items[i].Intangible = s.intangibleLoaded[i]
				}
			}
			return items, true
		}
	}
	return nil, false
}

// SetChargedProjectiles loads the stack with the projectiles given; an empty
// list resets the component to its default.
func (s *ItemStack) SetChargedProjectiles(items []ItemStack) {
	s.SetChargedProjectilesPatch(items, false)
}

// SetChargedProjectilesPatch replaces the minecraft:charged_projectiles patch,
// keeping an explicit empty patch when keepEmpty is true and otherwise
// resetting to the item's default.
func (s *ItemStack) SetChargedProjectilesPatch(items []ItemStack, keepEmpty bool) {
	if len(items) == 0 && !keepEmpty {
		s.ResetComponent(DataComponentChargedProjectiles)
		return
	}
	var value Buffer
	value.PutBundleContents(items)
	// A copied stack shares the Raw backing array; the write must not reach
	// the stack it came from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentChargedProjectiles, Data: value.Bytes()})
	s.intangibleLoaded = nil
	if slices.ContainsFunc(items, func(item ItemStack) bool { return item.Intangible }) {
		s.intangibleLoaded = make([]bool, len(items))
		for i, item := range items {
			s.intangibleLoaded[i] = item.Intangible
		}
	}
}
