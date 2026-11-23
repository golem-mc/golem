package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

var villagerFoodItems = map[int32]int32{}

func init() {
	for name, nutrition := range villagerFoodByName {
		if i, ok := ItemByName(name); ok {
			villagerFoodItems[int32(i)] = nutrition
		}
	}
}

// StackVillagerFood returns the stack's food points for a villager: its own
// villager_food nutrition, else the item's unless the patch removes it.
// vanilla: Villager.eatUntilFull, DataComponents.VILLAGER_FOOD
func StackVillagerFood(stack protocol.ItemStack) (int32, bool) {
	if stack.Empty() {
		return 0, false
	}
	if len(stack.Raw) != 0 {
		if values, ok := stack.RecordOf(protocol.DataComponentVillagerFood); ok {
			n, ok := values[0].(int32)
			return n, ok
		}
	}
	if stack.Removes(protocol.DataComponentVillagerFood) {
		return 0, false
	}
	n, ok := villagerFoodItems[stack.ItemID]
	return n, ok
}

// StackMobVisibility returns the visibility share the stack gives against
// the named entity type: its own mob_visibility, else the item's unless the
// patch removes it; false when the component does not name the type.
// vanilla: LivingEntity.getVisibilityPercent
func StackMobVisibility(stack protocol.ItemStack, entityType string) (float32, bool) {
	if stack.Empty() {
		return 0, false
	}
	if len(stack.Raw) != 0 {
		if values, ok := stack.RecordOf(protocol.DataComponentMobVisibility); ok {
			set, _ := values[0].(protocol.EntityTypeSet)
			share, _ := values[1].(float32)
			id, known := EntityTypeID(entityType)
			if !known {
				return 0, false
			}
			if set.Tag != "" {
				return share, EntityTypeInTag(id, set.Tag)
			}
			return share, slices.Contains(set.IDs, id)
		}
	}
	if stack.Removes(protocol.DataComponentMobVisibility) {
		return 0, false
	}
	types, share, ok := Item(stack.ItemID).MobVisibility()
	return share, ok && slices.Contains(types, entityType)
}
