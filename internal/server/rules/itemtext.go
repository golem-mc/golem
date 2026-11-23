package rules

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// ItemText is a stack's display name with a show-item hover of count (/give
// shows one whatever it gave, /item as many as it put). An empty stack shows
// as air. The name sits in an empty text's Extra, as vanilla nests it.
// vanilla: ItemStack.getDisplayName
func ItemText(stack protocol.ItemStack, count int32) protocol.Text {
	if stack.Empty() {
		return protocol.Text{
			Translate: "chat.square_brackets",
			With:      []protocol.Text{nested(protocol.Text{Translate: "block.minecraft.air"})},
		}
	}
	rarity := registry.RarityOf(stack.ItemID)
	if stack.Removes(protocol.DataComponentRarity) {
		rarity = registry.RarityCommon
	} else if own, ok := stack.RarityOf(); ok && own >= int32(registry.RarityCommon) && own <= int32(registry.RarityEpic) {
		rarity = registry.Rarity(own)
	}
	if stack.Enchanted() {
		// The bump vanilla's ItemStack.getRarity gives: an enchanted stack
		// is read one step rarer than its item, the rare capping at epic.
		if rarity < registry.RarityRare {
			rarity = registry.RarityRare
		} else {
			rarity = registry.RarityEpic
		}
	}
	inner := ItemName(stack)
	hover := &protocol.HoverItem{ID: "minecraft:unknown", Count: count}
	if name, ok := registry.ItemName(stack.ItemID); ok {
		hover.ID = name
	}
	if components := registry.StackComponents(stack); components != nil {
		hover.Components = components
	}
	return protocol.Text{
		Translate: "chat.square_brackets",
		Color:     rarity.Color(),
		With:      []protocol.Text{nested(inner)},
		HoverItem: hover,
	}
}

// nested wraps name as the empty text the square brackets enclose.
func nested(name protocol.Text) protocol.Text {
	return protocol.Text{Extra: []protocol.Text{name}}
}

// ItemName is a stack's hover name, with its custom name styled in italics.
// vanilla: ItemStack.getHoverName, ItemStack.getCustomName
func ItemName(stack protocol.ItemStack) protocol.Text {
	if stack.CustomName != nil {
		name := *stack.CustomName
		if name.Raw != nil {
			// A Raw text is only its tag; the italic goes on a parent its
			// own italic:false still overrides.
			return protocol.Text{Italic: true, Extra: []protocol.Text{name}}
		}
		name.Italic = true
		return name
	}
	if book, ok := stack.WrittenBookOf(); ok && strings.TrimSpace(book.Title) != "" {
		return protocol.Text{Text: book.Title}
	}
	return ItemTypeName(stack)
}

// ItemTypeName returns the stack's item name, including item-specific overrides.
// vanilla: ItemStack.getItemName, Item.getName
func ItemTypeName(stack protocol.ItemStack) protocol.Text {
	key, ok := registry.ItemTranslationKey(stack.ItemID)
	if !ok {
		return protocol.Text{Translate: "item.minecraft.unknown"}
	}
	switch registry.Item(stack.ItemID) {
	case registry.ItemPotion, registry.ItemSplashPotion, registry.ItemLingeringPotion, registry.ItemTippedArrow:
		return protocol.Text{Translate: key + ".effect." + potionSuffix(stack)}
	case registry.ItemCompass:
		if _, tracked := stack.LodestoneTrackerOf(); tracked {
			return protocol.Text{Translate: "item.minecraft.lodestone_compass"}
		}
	case registry.ItemPlayerHead:
		if name, hasName := stack.ProfileName(); hasName {
			return protocol.Text{
				Translate: "block.minecraft.player_head.named",
				With:      []protocol.Text{{Text: name}},
			}
		}
	case registry.ItemShield:
		if dye, hasBase := stack.BaseColorOf(); hasBase {
			return protocol.Text{Translate: key + "." + registry.DyeNames[dye]}
		}
	}
	if name, ok := stack.ItemNameOf(); ok {
		return name
	}
	if stack.Removes(protocol.DataComponentItemName) {
		return protocol.Text{}
	}
	return protocol.Text{Translate: key}
}

// potionSuffix names a potion item's contents in its translation key: the
// custom name, else the potion's name (Potion.name drops a long_ or strong_
// prefix), else "empty", which a stack without potion_contents also is.
// vanilla: PotionContents.getName
func potionSuffix(stack protocol.ItemStack) string {
	contents, ok := stack.PotionContentsOf()
	if !ok {
		return "empty"
	}
	if contents.CustomName != nil {
		return *contents.CustomName
	}
	if contents.Potion == protocol.NoPotion {
		return "empty"
	}
	name, known := registry.PotionName(contents.Potion)
	if !known {
		return "empty"
	}
	name = strings.TrimPrefix(name, "minecraft:")
	if rest, cut := strings.CutPrefix(name, "long_"); cut {
		return rest
	}
	return strings.TrimPrefix(name, "strong_")
}
