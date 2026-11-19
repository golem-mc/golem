package protocol

import "slices"

// The minecraft:charged_projectiles component is held as the bytes it arrived
// in, like the bundle's, whose codec it shares: a list of item templates.

// ChargedProjectiles returns the projectiles a crossbow stack is loaded with;
// nil for an unloaded one.
func (s ItemStack) ChargedProjectiles() []ItemStack {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentChargedProjectiles {
			items, ok := bundleItemsOf(raw.Data)
			if !ok {
				return nil
			}
			if len(s.intangibleLoaded) == len(items) {
				for i := range items {
					items[i].Intangible = s.intangibleLoaded[i]
				}
			}
			return items
		}
	}
	return nil
}

// SetChargedProjectiles loads the stack with the projectiles given; an empty
// list removes the component, the absence an unloaded crossbow is stored as.
func (s *ItemStack) SetChargedProjectiles(items []ItemStack) {
	if len(items) == 0 {
		s.dropRaw(DataComponentChargedProjectiles)
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
