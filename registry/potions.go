package registry

// The potion table's accessors: potions_gen.go holds the built-in
// minecraft:potion registry itself, its entries in the order the wire's IDs
// number them.

// PotionID returns the ID a potion_contents component names the potion by,
// or false for a name the registry does not carry.
func PotionID(name string) (int32, bool) {
	name = Qualify(name)
	for id, potion := range potions {
		if potion.Name == name {
			return int32(id), true
		}
	}
	return 0, false
}

// MustPotion is PotionID for names this package is generated with.
func MustPotion(name string) int32 {
	id, ok := PotionID(name)
	if !ok {
		panic("unknown potion " + name)
	}
	return id
}

// PotionName returns the name a potion ID travels under, or false for an ID
// the registry does not carry.
func PotionName(id int32) (string, bool) {
	if id < 0 || int(id) >= len(potions) {
		return "", false
	}
	return potions[id].Name, true
}

// PotionEffects returns the effects drinking the potion named by the ID a
// potion_contents component carries gives, in the order the potion hands
// them out. An ID the registry does not carry is nothing: the wire reader
// has already refused the stack that carried it.
func PotionEffects(id int32) []EffectSpec {
	if id < 0 || int(id) >= len(potions) {
		return nil
	}
	return potions[id].Effects
}

// PotionTagMembers returns a potion tag's names in registry tag order.
func PotionTagMembers(tag string) ([]string, bool) {
	for _, r := range tagsNow.Load().builtin {
		if r.Registry != "minecraft:potion" {
			continue
		}
		for _, t := range r.Tags {
			if t.Name != tag {
				continue
			}
			names := make([]string, 0, len(t.IDs))
			for _, id := range t.IDs {
				if name, ok := PotionName(id); ok {
					names = append(names, name)
				}
			}
			return names, true
		}
	}
	return nil, false
}
