package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ComponentID is the registry ID of a data component type by name.
func ComponentID(name string) (int32, bool) {
	id, ok := ItemComponentIDs[name]
	return id, ok
}

// componentNames indexes ItemComponentIDs by ID once.
var componentNames = func() map[int32]string {
	names := make(map[int32]string, len(ItemComponentIDs))
	for name, id := range ItemComponentIDs {
		names[id] = name
	}
	return names
}()

// addDisplayComponents writes the item_model, tooltip_style, rarity,
// tooltip_display and break_sound components of a stack.
func addDisplayComponents(stack protocol.ItemStack, components nbt.Compound) nbt.Compound {
	display := nbt.Compound{}
	if model, ok := stack.ItemModelOf(); ok {
		display["minecraft:item_model"] = nbt.String(model)
	}
	if style, ok := stack.TooltipStyleOf(); ok {
		display["minecraft:tooltip_style"] = nbt.String(style)
	}
	// vanilla: PatchedDataComponentMap.set stores no patch for the prototype's value
	if rarity, ok := stack.RarityOf(); ok && rarity != int32(RarityOf(stack.ItemID)) {
		if name, ok := rarityName(Rarity(rarity)); ok {
			display["minecraft:rarity"] = nbt.String(name)
		}
	}
	if hide, hidden := stack.HiddenTooltipOf(); hide || len(hidden) > 0 {
		tooltip := nbt.NewOrderedCompound(2)
		if hide {
			tooltip.Set("hide_tooltip", nbt.Byte(1))
		}
		names := make([]nbt.Tag, 0, len(hidden))
		for _, id := range hidden {
			if name, ok := componentNames[id]; ok {
				names = append(names, nbt.String(name))
			}
		}
		if len(names) > 0 {
			tooltip.Set("hidden_components", nbt.List{Elem: nbt.TypeString, Items: names})
		}
		display["minecraft:tooltip_display"] = tooltip
	}
	if sound, ok := stack.BreakSoundOf(); ok {
		if tag, ok := soundHolderTag(sound); ok {
			display["minecraft:break_sound"] = tag
		}
	}
	for k, v := range display {
		if components == nil {
			components = nbt.Compound{}
		}
		components[k] = v
	}
	return components
}

var rarityNames = [...]string{"common", "uncommon", "rare", "epic"}

func rarityName(r Rarity) (string, bool) {
	if r < 0 || int(r) >= len(rarityNames) {
		return "", false
	}
	return rarityNames[r], true
}

// readDisplayComponent is readComponentKnown for item_model, tooltip_style,
// rarity, tooltip_display and break_sound.
func readDisplayComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:item_model", "minecraft:tooltip_style":
		id, ok := value.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(id)) {
			return false, true
		}
		if name == "minecraft:item_model" {
			// vanilla: Item.Properties.model defaults to the item's own ID
			if own, _ := ItemName(stack.ItemID); Qualify(string(id)) != own {
				stack.SetItemModel(Qualify(string(id)))
			}
		} else {
			stack.SetTooltipStyle(Qualify(string(id)))
		}
	case "minecraft:rarity":
		text, ok := value.(nbt.String)
		if !ok {
			return false, true
		}
		for i, n := range rarityNames {
			if n == string(text) {
				if Rarity(i) != RarityOf(stack.ItemID) {
					stack.SetRarity(int32(i))
				}
				return true, true
			}
		}
		return false, true
	case "minecraft:tooltip_display":
		compound, ok := nbt.CompoundView(value)
		if !ok {
			return false, true
		}
		hide := false
		if tag, present := compound["hide_tooltip"]; present {
			b, ok := nbt.ByteValue(tag)
			if !ok {
				return false, true
			}
			hide = b != 0
		}
		var ids []int32
		if tag, present := compound["hidden_components"]; present {
			list, ok := tag.(nbt.List)
			if !ok {
				return false, true
			}
			for _, item := range list.Items {
				n, ok := item.(nbt.String)
				if !ok {
					return false, true
				}
				id, ok := ComponentID(Qualify(string(n)))
				if !ok {
					return false, true
				}
				// vanilla: TooltipDisplay keeps a ReferenceLinkedOpenHashSet
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		if hide || len(ids) > 0 {
			stack.SetTooltipDisplay(hide, ids)
		}
	case "minecraft:break_sound":
		sound, ok := soundHolderOf(value)
		if !ok {
			return false, true
		}
		if id, ok := SoundEventID(defaultBreakSound(Item(stack.ItemID))); ok && sound == (protocol.SoundHolder{ID: id}) {
			return true, true
		}
		var out protocol.Buffer
		putSoundHolder(&out, sound)
		stack.SetRaw(protocol.RawComponent{Type: protocol.DataComponentBreakSound, Data: out.Bytes()})
	default:
		return false, false
	}
	return true, true
}

// defaultBreakSound is the break_sound of an item's prototype.
// vanilla: DataComponents.COMMON_ITEM_COMPONENTS, Items.SHIELD, Item.Properties.wolfArmor
func defaultBreakSound(item Item) string {
	switch item {
	case ItemShield:
		return "minecraft:item.shield.break"
	case ItemWolfArmor:
		return "minecraft:item.wolf_armor.break"
	}
	return "minecraft:entity.item.break"
}
