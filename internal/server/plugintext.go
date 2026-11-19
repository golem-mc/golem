package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

var pluginClickFields = [...]struct{ action, key string }{
	{"open_url", "url"}, {"run_command", "command"}, {"suggest_command", "command"},
	{"change_page", "page"}, {"copy_to_clipboard", "value"}, {"show_dialog", "dialog"}, {"custom", "id"},
}

// pluginTextJSON converts API components at the protocol boundary, retaining
// explicit false decorations rather than turning them into inherited values.
// vanilla: ComponentSerialization.CODEC, Style.Serializer.MAP_CODEC
func pluginTextJSON(t plugin.Text) map[string]any {
	return pluginJSONValue(pluginTextValue(t)).(map[string]any)
}

func pluginTextValue(t plugin.Text) map[string]any {
	m := make(map[string]any)
	switch {
	case t.Translate != "":
		m["translate"] = t.Translate
		if t.Fallback != "" {
			m["fallback"] = t.Fallback
		}
	case t.Keybind != "":
		m["keybind"] = t.Keybind
	default:
		m["text"] = t.Literal
	}
	for key, texts := range map[string][]plugin.Text{"with": t.Args, "extra": t.Children} {
		if len(texts) == 0 {
			continue
		}
		values := make([]any, len(texts))
		for i, text := range texts {
			values[i] = pluginTextValue(text)
		}
		m[key] = values
	}
	style := t.Style
	if style.Color != "" {
		m["color"] = style.Color
	}
	if style.Shadow != nil {
		m["shadow_color"] = json.Number(strconv.FormatInt(int64(int32(*style.Shadow)), 10))
	}
	for key, flag := range map[string]*bool{"bold": style.Bold, "italic": style.Italic, "underlined": style.Underlined, "strikethrough": style.Strikethrough, "obfuscated": style.Obfuscated} {
		if flag != nil {
			m[key] = *flag
		}
	}
	if style.Font != (plugin.Key{}) {
		m["font"] = style.Font.String()
	}
	if style.Insertion != "" {
		m["insertion"] = style.Insertion
	}
	if click := style.Click; click != nil {
		if int(click.Action) < len(pluginClickFields) {
			field := pluginClickFields[click.Action]
			value := any(click.Value)
			if click.Action == plugin.ChangePage {
				value = json.Number(click.Value)
			}
			event := map[string]any{"action": field.action, field.key: value}
			if click.Action == plugin.Custom && len(click.Payload) > 0 {
				if tag, _, err := nbt.DecodeNetwork(click.Payload); err == nil {
					event["payload"] = tag
				}
			}
			m["click_event"] = event
		}
	}
	if hover := style.Hover; hover != nil {
		switch {
		case hover.Entity != nil:
			e := hover.Entity
			event := map[string]any{"action": "show_entity", "id": e.Type.Name(), "uuid": e.UUID.String()}
			if e.Name != nil {
				event["name"] = pluginTextValue(*e.Name)
			}
			m["hover_event"] = event
		case hover.Item != nil:
			event := map[string]any{"action": "show_item", "id": hover.Item.Item.Name(), "count": hover.Item.Count}
			if components := pluginItemComponents(hover.Item.Patch); len(components) > 0 {
				event["components"] = components
			}
			m["hover_event"] = event
		case hover.Text != nil:
			m["hover_event"] = map[string]any{"action": "show_text", "value": pluginTextValue(*hover.Text)}
		}
	}
	return m
}

func pluginProtocolText(t plugin.Text) protocol.Text {
	tag, err := pluginValueTag(pluginTextValue(t))
	if err != nil {
		return protocol.Text{Text: t.PlainString()}
	}

	// Raw is the protocol model's lossless form for optional style fields and
	// content codecs. Keep it even when a zero-valued field looks like no event.
	if !protocol.RawTextFits(tag) {
		return protocol.Text{Text: t.PlainString()}
	}
	return protocol.Text{Raw: tag}
}

// pluginItemComponents converts the API's item patch to codec fields.
func pluginItemComponents(p plugin.ItemPatch) map[string]any {
	c := make(map[string]any)
	for key, text := range map[string]*plugin.Text{"custom_name": p.CustomName, "item_name": p.ItemName} {
		if text != nil {
			c["minecraft:"+key] = pluginTextValue(*text)
		}
	}
	if len(p.Lore) > 0 {
		lore := make([]any, len(p.Lore))
		for i, line := range p.Lore {
			lore[i] = pluginTextValue(line)
		}
		c["minecraft:lore"] = lore
	}
	for key, value := range map[string]*int{"damage": p.Damage, "max_damage": p.MaxDamage, "max_stack_size": p.MaxStackSize} {
		if value != nil {
			c["minecraft:"+key] = *value
		}
	}
	if p.Unbreakable != nil {
		if *p.Unbreakable {
			c["minecraft:unbreakable"] = map[string]any{}
		} else {
			c["!minecraft:unbreakable"] = map[string]any{}
		}
	}
	for key, levels := range map[string]map[plugin.Enchantment]int{"enchantments": p.Enchantments, "stored_enchantments": p.StoredEnchantments} {
		if len(levels) == 0 {
			continue
		}
		enchants := make(map[string]any, len(levels))
		for enchant, level := range levels {
			if name, ok := registry.EnchantmentName(int32(enchant)); ok {
				enchants[name] = level
			}
		}
		c["minecraft:"+key] = enchants
	}
	if p.GlintOverride != nil {
		c["minecraft:enchantment_glint_override"] = *p.GlintOverride
	}
	if p.Model != nil {
		c["minecraft:item_model"] = p.Model.String()
	}
	if d := p.CustomModelData; d != nil {
		data := make(map[string]any)
		if len(d.Floats) > 0 {
			data["floats"] = d.Floats
		}
		if len(d.Flags) > 0 {
			data["flags"] = d.Flags
		}
		if len(d.Strings) > 0 {
			data["strings"] = d.Strings
		}
		if len(d.Colors) > 0 {
			data["colors"] = d.Colors
		}
		c["minecraft:custom_model_data"] = data
	}
	if p.Rarity != nil {
		names := [...]string{"common", "uncommon", "rare", "epic"}
		if i := int(*p.Rarity); i >= 0 && i < len(names) {
			c["minecraft:rarity"] = names[i]
		}
	}
	if p.HideTooltip != nil {
		c["minecraft:tooltip_display"] = map[string]any{"hide_tooltip": *p.HideTooltip}
	}
	if p.DyedColor != nil {
		c["minecraft:dyed_color"] = *p.DyedColor
	}
	if p.Profile != nil {
		profile := map[string]any{"id": nbt.UUIDToIntArray(p.Profile.ID), "name": p.Profile.Name}
		if len(p.Profile.Properties) > 0 {
			properties := make([]any, len(p.Profile.Properties))
			for i, prop := range p.Profile.Properties {
				value := map[string]any{"name": prop.Name, "value": prop.Value}
				if prop.Signature != "" {
					value["signature"] = prop.Signature
				}
				properties[i] = value
			}
			profile["properties"] = properties
		}
		c["minecraft:profile"] = profile
	}
	if len(p.Data) > 0 {
		data := make(map[string]any, len(p.Data))
		for key, value := range p.Data {
			data[key.String()] = value
		}
		c["minecraft:custom_data"] = data
	}
	for key, snbt := range p.Other {
		if snbt == "" {
			c["!"+key.String()] = map[string]any{}
			continue
		}
		if tag, _, err := nbt.ParseValueTag(snbt); err == nil {
			c[key.String()] = tag
		}
	}
	return c
}

// pluginJSONValue adapts typed NBT leaves only for the JSON status response.
func pluginJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[key] = pluginJSONValue(value)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, value := range v {
			out[i] = pluginJSONValue(value)
		}
		return out
	case *nbt.OrderedCompound:
		return pluginJSONValue(v.Entries())
	case nbt.Compound:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[key] = pluginJSONValue(value)
		}
		return out
	case nbt.List:
		out := make([]any, len(v.Items))
		for i, value := range v.Items {
			out[i] = pluginJSONValue(nbt.ListElement(value))
		}
		return out
	case []byte:
		return pluginJSONValue(nbt.ByteArray(v))
	case nbt.ByteArray:
		out := make([]int8, len(v))
		for i, value := range v {
			out[i] = int8(value)
		}
		return out
	default:
		return value
	}
}

// pluginValueTag retains numeric widths and arrays in arbitrary click payloads
// and item patches; a JSON round trip would turn them into ints and lists.
func pluginValueTag(value any) (nbt.Tag, error) {
	switch v := value.(type) {
	case nbt.Tag:
		return v, nil
	case json.Number:
		return protocol.JSONNumberTag(v)
	case []byte:
		return nbt.ByteArray(v), nil
	case []int32:
		return nbt.IntArray(v), nil
	case []int64:
		return nbt.LongArray(v), nil
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return protocol.BoolTag(v.Bool()), nil
	case reflect.String:
		return nbt.String(v.String()), nil
	case reflect.Int8:
		return nbt.Byte(v.Int()), nil
	case reflect.Int16:
		return nbt.Short(v.Int()), nil
	case reflect.Int, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		if v.CanInt() {
			return nbt.Int(v.Int()), nil
		}
		return nbt.Int(v.Uint()), nil
	case reflect.Int64:
		return nbt.Long(v.Int()), nil
	case reflect.Float32:
		return nbt.Float(v.Float()), nil
	case reflect.Float64:
		return nbt.Double(v.Float()), nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			break
		}
		out := make(nbt.Compound, v.Len())
		it := v.MapRange()
		for it.Next() {
			tag, err := pluginValueTag(it.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[it.Key().String()] = tag
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		items := make([]nbt.Tag, v.Len())
		for i := range items {
			tag, err := pluginValueTag(v.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			items[i] = tag
		}
		return nbt.NewList(items), nil
	}
	return nil, fmt.Errorf("unsupported plugin component value %T", value)
}
