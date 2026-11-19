package registry

// BookSlotKind selects the contents of a recipe book slot display.
type BookSlotKind uint8

const (
	BookSlotIngredient BookSlotKind = iota
	BookSlotStack
	BookSlotPotion
	BookSlotComponent
	BookSlotDyed
	BookSlotComposite
)

// BookSlot describes an ingredient, result, or nested slot display.
// vanilla: SlotDisplay
type BookSlot struct {
	Kind          BookSlotKind
	Ingredient    BookIngredient
	ItemID, Count int32
	Component     int32
	Children      []BookSlot
}

// CraftingSlots carries displays whose visuals differ from placement ingredients.
type CraftingSlots struct {
	Squares []BookSlot
	Result  BookSlot
}

// CraftingDisplaysOfName returns all displays of a recipe, in display ID order.
func CraftingDisplaysOfName(name string) []CraftingDisplay {
	set := recipes()
	i, ok := set.displayByName[name]
	if !ok {
		return nil
	}
	end := i + 1
	for end < len(set.displays) && set.displays[end].Name == name {
		end++
	}
	return set.displays[i:end]
}
