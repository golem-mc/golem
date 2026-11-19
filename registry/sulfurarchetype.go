package registry

import (
	"slices"
	"sync/atomic"
)

// SulfurArchetype is one sulfur_cube_archetype entry: which items a cube
// holding the item takes the archetype from, and what it then does.
// vanilla: SulfurCubeArchetype
type SulfurArchetype struct {
	// Name is the registry name, such as "minecraft:bouncy".
	Name string
	// ItemTag (a tag name without '#') or Items is the items holder set.
	ItemTag   string
	Items     []Item
	Modifiers []SulfurModifier
	Buoyant   bool
	// Explosion and Contact are nil for an archetype with neither.
	Explosion *SulfurExplosion
	Contact   *SulfurContact
	// KnockbackHorizontal and KnockbackVertical are the hit shove's powers.
	KnockbackHorizontal, KnockbackVertical float32
	HitSound, PushSound                    Sound
	// PushThreshold is the shove speed that makes a push sound; PushCooldown is
	// the seconds between two.
	PushThreshold, PushCooldown float32
}

// SulfurModifier is one attribute modifier a cube holding the block carries.
type SulfurModifier struct {
	Attribute AttributeID
	ID        string
	Amount    float64
	Operation AttributeOperation
}

// SulfurExplosion is a cube's fuse and blast: Fuse in ticks, Power the blast radius.
type SulfurExplosion struct {
	Power      int
	CausesFire bool
	Fuse       int
}

// SulfurContact is the damage a cube deals to what it touches, a constant when
// Min equals Max and uniform between them otherwise.
type SulfurContact struct {
	DamageType        DamageType
	Min, Max          float32
	AttributeToSource bool
}

// Matches reports whether the item is in the archetype's items holder set.
func (a *SulfurArchetype) Matches(item Item) bool {
	if a.ItemTag != "" {
		return item.InTag(ItemTagOf(a.ItemTag))
	}
	return slices.Contains(a.Items, item)
}

var sulfurArchetypesNow atomic.Pointer[[]SulfurArchetype]

// SulfurArchetypes are the archetypes in registry order: the built-in ones,
// replaced or joined by the data packs'.
func SulfurArchetypes() []SulfurArchetype {
	if p := sulfurArchetypesNow.Load(); p != nil {
		return *p
	}
	return builtinSulfurArchetypes
}

// ApplySulfurArchetypes makes the data packs' archetypes, by registry name,
// replace and join the built-in ones, sorted by name; nil restores them.
func ApplySulfurArchetypes(packs map[string]SulfurArchetype) {
	if len(packs) == 0 {
		sulfurArchetypesNow.Store(nil)
		return
	}
	merged := make(map[string]SulfurArchetype, len(builtinSulfurArchetypes)+len(packs))
	for _, a := range builtinSulfurArchetypes {
		merged[a.Name] = a
	}
	for name, a := range packs {
		a.Name = name
		merged[name] = a
	}
	list := make([]SulfurArchetype, 0, len(merged))
	for _, a := range merged {
		list = append(list, a)
	}
	slices.SortFunc(list, func(a, b SulfurArchetype) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	sulfurArchetypesNow.Store(&list)
}

func mustItem(name string) Item {
	item, ok := ItemByName(name)
	if !ok {
		panic("registry: no item " + name)
	}
	return item
}

func mustAttributeID(name string) AttributeID {
	id, ok := AttributeIDByName(name)
	if !ok {
		panic("registry: no attribute " + name)
	}
	return id
}

// AttributeIDByName is AttributeByName's index into Attributes.
func AttributeIDByName(name string) (AttributeID, bool) {
	for i, a := range Attributes {
		if a.Name == name {
			return AttributeID(i), true
		}
	}
	return 0, false
}

func mustAttributeOperation(name string) AttributeOperation {
	op, ok := AttributeOperationByName(name)
	if !ok {
		panic("registry: no attribute operation " + name)
	}
	return op
}
