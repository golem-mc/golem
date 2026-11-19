package server

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginItemPatchOf reads item patches; unmodelled components retain
// their typed SNBT rather than being discarded at the API boundary.
// vanilla: ItemStackTemplate.MAP_CODEC
func pluginItemPatchOf(c nbt.Compound) (plugin.ItemPatch, bool) {
	var p plugin.ItemPatch
	for name, value := range c {
		key, err := plugin.ParseKey(strings.TrimPrefix(name, "!"))
		if err != nil {
			return p, false
		}

		if !pluginItemPatchField(&p, name, value) {
			if p.Other == nil {
				p.Other = make(map[plugin.Key]string)
			}
			if strings.HasPrefix(name, "!") {
				p.Other[key] = ""
			} else {
				p.Other[key] = nbt.Stringify(value)
			}
		}
	}
	return p, true
}

func pluginItemPatchField(p *plugin.ItemPatch, name string, value nbt.Tag) bool {
	handled := false
	switch name {
	case "minecraft:custom_name", "minecraft:item_name":
		text, ok := pluginTextOf(value)
		if !ok {
			return false
		}
		if name == "minecraft:custom_name" {
			p.CustomName = &text
		} else {
			p.ItemName = &text
		}
		handled = true
	case "minecraft:lore":
		list, ok := value.(nbt.List)
		if !ok {
			return false
		}
		for _, line := range list.Items {
			text, ok := pluginTextOf(nbt.ListElement(line))
			if !ok {
				return false
			}
			p.Lore = append(p.Lore, text)
		}
		handled = true
	case "minecraft:damage", "minecraft:max_damage", "minecraft:max_stack_size":
		n, ok := nbt.IntValue(value)
		if !ok {
			return false
		}
		switch name {
		case "minecraft:damage":
			p.Damage = new(int(n))
		case "minecraft:max_damage":
			p.MaxDamage = new(int(n))
		case "minecraft:max_stack_size":
			p.MaxStackSize = new(int(n))
		}
		handled = true
	case "minecraft:unbreakable", "!minecraft:unbreakable":
		p.Unbreakable = new(name == "minecraft:unbreakable")
		handled = true
	case "minecraft:enchantment_glint_override":
		p.GlintOverride = new(protocol.FlagOn(value))
		handled = true
	case "minecraft:item_model":
		model, ok := pluginString(value)
		if !ok {
			return false
		}
		parsed, err := plugin.ParseKey(model)
		if err != nil {
			return false
		}
		p.Model = &parsed
		handled = true
	case "minecraft:dyed_color":
		color, ok := nbt.IntValue(value)
		if !ok {
			return false
		}
		p.DyedColor = &color
		handled = true
	case "minecraft:rarity":
		for i, rarity := range []string{"common", "uncommon", "rare", "epic"} {
			if value == nbt.String(rarity) {
				p.Rarity = new(registry.Rarity(i))
				handled = true
				break
			}
		}
	case "minecraft:tooltip_display":
		fields, ok := nbt.CompoundView(value)
		if !ok {
			return false
		}
		if flag, present := fields["hide_tooltip"]; present {
			p.HideTooltip = new(protocol.FlagOn(flag))
			handled = len(fields) == 1
		}
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		fields, ok := nbt.CompoundView(value)
		if !ok {
			return false
		}
		levels := make(map[plugin.Enchantment]int, len(fields))
		handled = true
		for key, tag := range fields {
			id, ok := registry.EnchantmentID(key)
			n, numeric := nbt.IntValue(tag)
			if !ok || !numeric {
				handled = false
				break
			}
			levels[plugin.Enchantment(id)] = int(n)
		}
		if handled {
			if name == "minecraft:enchantments" {
				p.Enchantments = levels
			} else {
				p.StoredEnchantments = levels
			}
		}
	case "minecraft:custom_model_data":
		fields, ok := nbt.CompoundView(value)
		if !ok {
			return false
		}
		model := &plugin.CustomModelData{}
		handled = true
		for key, value := range fields {
			switch key {
			case "floats", "flags", "strings", "colors":
			default:
				return false
			}
			tags, ok := pluginListTags(value)
			if !ok {
				return false
			}
			for _, tag := range tags {
				tag = nbt.ListElement(tag)
				n, numeric := nbt.NumberValue(tag)
				switch key {
				case "floats":
					if !numeric {
						return false
					}
					model.Floats = append(model.Floats, float32(n))
				case "flags":
					if !numeric {
						return false
					}
					model.Flags = append(model.Flags, n != 0)
				case "strings":
					s, ok := pluginString(tag)
					if !ok {
						return false
					}
					model.Strings = append(model.Strings, s)
				case "colors":
					if !numeric {
						return false
					}
					model.Colors = append(model.Colors, int32(n))
				}
			}
		}
		p.CustomModelData = model
	case "minecraft:profile":
		fields, ok := nbt.CompoundView(value)
		if !ok {
			return false
		}
		// Skin patches and alternate property forms need the original component.
		// vanilla: ResolvableProfile.CODEC, ExtraCodecs.PROPERTY_MAP
		for key := range fields {
			switch key {
			case "id", "name", "properties":
			default:
				return false
			}
		}
		id, ok := pluginUUID(fields, "id")
		if !ok {
			return false
		}
		profile := &plugin.Profile{ID: id}
		if name, present := fields["name"]; present {
			profile.Name, ok = pluginString(name)
			if !ok {
				return false
			}
		}
		var properties nbt.List
		if tag, present := fields["properties"]; present {
			properties, ok = tag.(nbt.List)
			if !ok {
				return false
			}
		}
		for _, tag := range properties.Items {
			prop, ok := nbt.CompoundView(nbt.ListElement(tag))
			if !ok {
				return false
			}
			for key := range prop {
				if key != "name" && key != "value" && key != "signature" {
					return false
				}
			}
			name, nameOK := pluginString(prop["name"])
			value, valueOK := pluginString(prop["value"])
			if !nameOK || !valueOK {
				return false
			}
			var signature string
			if tag, present := prop["signature"]; present {
				signature, ok = pluginString(tag)
				if !ok {
					return false
				}
			}
			profile.Properties = append(profile.Properties, plugin.ProfileProperty{Name: name, Value: value, Signature: signature})
		}
		p.Profile = profile
		handled = true
	case "minecraft:custom_data":
		fields, ok := nbt.CompoundView(value)
		if !ok {
			return false
		}
		data := make(map[plugin.Key]any, len(fields))
		handled = true
		for name, value := range fields {
			key, err := plugin.ParseKey(name)
			if err != nil || key.String() != name {
				handled = false
				break
			}
			data[key] = pluginTagValue(value)
		}
		if handled {
			p.Data = data
		}
	}
	return handled
}

func pluginListTags(tag nbt.Tag) ([]nbt.Tag, bool) {
	switch v := tag.(type) {
	case nbt.List:
		return v.Items, true
	case nbt.ByteArray:
		out := make([]nbt.Tag, len(v))
		for i, n := range v {
			out[i] = nbt.Byte(n)
		}
		return out, true
	case nbt.IntArray:
		out := make([]nbt.Tag, len(v))
		for i, n := range v {
			out[i] = nbt.Int(n)
		}
		return out, true
	case nbt.LongArray:
		out := make([]nbt.Tag, len(v))
		for i, n := range v {
			out[i] = nbt.Long(n)
		}
		return out, true
	}
	return nil, false
}

func pluginTagValue(tag nbt.Tag) any {
	switch v := tag.(type) {
	case nbt.Byte:
		return int8(v)
	case nbt.Short:
		return int16(v)
	case nbt.Int:
		return int32(v)
	case nbt.Long:
		return int64(v)
	case nbt.Float:
		return float32(v)
	case nbt.Double:
		return float64(v)
	case nbt.String:
		return string(v)
	case nbt.ByteArray:
		return append([]byte(nil), v...)
	case nbt.IntArray:
		return append([]int32(nil), v...)
	case nbt.LongArray:
		return append([]int64(nil), v...)
	case nbt.List:
		out := make([]any, len(v.Items))
		for i, value := range v.Items {
			out[i] = pluginTagValue(nbt.ListElement(value))
		}
		return out
	case *nbt.OrderedCompound:
		return pluginTagValue(v.Entries())
	case nbt.Compound:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[key] = pluginTagValue(value)
		}
		return out
	}
	return nil
}
