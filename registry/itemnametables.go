package registry

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

var equippableItems [ItemCount]EquippableItem
var itemDamageResistance [ItemCount]DamageTypeTagID

type mobVisibilityItem struct {
	types      []string
	visibility float32
}

var mobVisibilityItems [ItemCount]*mobVisibilityItem

func init() {
	for i := range itemDamageResistance {
		itemDamageResistance[i] = noDamageTypeTag
	}
	for name := range fireResistantItems {
		if i, ok := ItemByName(name); ok {
			itemDamageResistance[i] = DamageTypeTagIDIsFire
		}
	}
	for name := range explosionResistantItems {
		if i, ok := ItemByName(name); ok && itemDamageResistance[i] == noDamageTypeTag {
			itemDamageResistance[i] = DamageTypeTagIDIsExplosion
		}
	}

	for name, item := range mobVisibilityByName {
		if i, ok := ItemByName(name); ok {
			mobVisibilityItems[i] = &item
		}
	}
	for name, item := range Equippable {
		if i, ok := ItemByName(name); ok {
			equippableItems[i] = item
		}
	}
}

// Equippable returns the item's equippable component, false for an invalid ID
// or an item with no equippable component.
func (i Item) Equippable() (EquippableItem, bool) {
	if i < 0 || i >= ItemCount {
		return EquippableItem{}, false
	}
	item := equippableItems[i]
	// The generated map contains only components that name a slot.
	return item, item.Slot != ""
}

// StackEquippable returns the stack's own equippable component when it
// carries one, else its item's; false when the stack's patch removes it or
// neither names a slot.
// vanilla: ItemStack.get(DataComponents.EQUIPPABLE)
func StackEquippable(stack protocol.ItemStack) (EquippableItem, bool) {
	// Inlined, so neither the stack nor the component is copied on the way.
	if e := stackEquippable(&stack); e != nil {
		return *e, true
	}
	return EquippableItem{}, false
}

// stackEquippable is StackEquippable's component, nil for none: the item's
// table entry, or a new value for the stack's own.
func stackEquippable(stack *protocol.ItemStack) *EquippableItem {
	if stack.Removes(protocol.DataComponentEquippable) {
		return nil
	}
	if len(stack.Raw) > 0 {
		if own, ok := stack.EquippableOf(); ok {
			return ownEquippable(own)
		}
	}
	if stack.ItemID < 0 || stack.ItemID >= int32(ItemCount) || equippableItems[stack.ItemID].Slot == "" {
		return nil
	}
	return &equippableItems[stack.ItemID]
}

// ownEquippable converts a stack's own component to EquippableItem's shape.
func ownEquippable(own protocol.Equippable) *EquippableItem {
	return &EquippableItem{
		Slot:            own.Slot,
		Swappable:       own.Swappable,
		Entities:        entitySetString(own.AllowedEntities),
		EquipSound:      own.EquipSound,
		CanBeSheared:    own.CanBeSheared,
		ShearingSound:   own.ShearingSound,
		Dispensable:     own.Dispensable,
		DamageOnHurt:    own.DamageOnHurt,
		EquipOnInteract: own.EquipOnInteract,
	}
}

// entitySetString writes a holder set of entity types in EquippableItem's
// Entities shape.
func entitySetString(set protocol.EntityTypeSet) string {
	if !set.Present {
		return ""
	}
	if set.Tag != "" {
		return "#" + set.Tag
	}
	names := make([]string, 0, len(set.IDs))
	for _, id := range set.IDs {
		if int(id) < len(EntityTypes) {
			names = append(names, EntityTypes[id])
		}
	}
	return strings.Join(names, ",")
}

// MobVisibility returns the item's default mob_visibility component, ignoring
// stack component patches. The returned entity type names must not be modified.
func (i Item) MobVisibility() (types []string, visibility float32, ok bool) {
	if i < 0 || i >= ItemCount || mobVisibilityItems[i] == nil {
		return nil, 0, false
	}
	v := mobVisibilityItems[i]
	return v.types, v.visibility, true
}
