package world

import (
	"encoding/json"
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// textComponentVersion is the data version from which a sign's messages and a
// block entity's CustomName are text components rather than JSON strings.
// vanilla: DataFixers.addFixers (UnflattenTextComponentFix)
const textComponentVersion = 4290

// unflattenText reads a JSON string as the component it holds; a string that is
// no JSON component stays a literal line.
// vanilla: UnflattenTextComponentFix.parseJSON
func unflattenText(tag nbt.Tag) nbt.Tag {
	s, ok := tag.(nbt.String)
	if !ok {
		return tag
	}
	parsed, _, err := protocol.ParseTextJSON(string(s))
	if err != nil {
		return tag
	}
	return parsed
}

// unflattenMessages converts a message list's JSON strings to components.
func unflattenMessages(list nbt.List) nbt.List {
	items := make([]nbt.Tag, len(list.Items))
	allStrings := true
	for i, item := range list.Items {
		item = unflattenText(nbt.ListElement(item))
		if _, ok := item.(nbt.String); !ok {
			allStrings = false
		}
		items[i] = item
	}
	return mixedList(items, allStrings)
}

// mixedList builds a list of components, wrapping a non-compound one in the ""
// compound a list of mixed types stores it in.
func mixedList(items []nbt.Tag, allStrings bool) nbt.List {
	if allStrings {
		return nbt.List{Elem: nbt.TypeString, Items: items}
	}
	for i, item := range items {
		if _, ok := nbt.CompoundView(item); !ok {
			items[i] = nbt.Compound{"": item}
		}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// unflattenBlockEntityText applies UnflattenTextComponentFix to a block entity:
// sign messages and a CustomName.
func unflattenBlockEntityText(entries nbt.Compound) {
	if name, ok := entries["CustomName"]; ok {
		entries["CustomName"] = unflattenText(name)
	}
	for _, side := range []string{"front_text", "back_text"} {
		text, ok := nbt.Get[nbt.Compound](entries, side)
		if !ok {
			continue
		}
		for _, key := range []string{"messages", "filtered_messages"} {
			if list, ok := nbt.Get[nbt.List](text, key); ok {
				text[key] = unflattenMessages(list)
			}
		}
	}
}

// flattenText writes a component as the JSON string a file older than
// textComponentVersion stores.
func flattenText(tag nbt.Tag) nbt.Tag {
	data, err := json.Marshal(textJSONValue(nbt.ListElement(tag)))
	if err != nil {
		return nbt.String(emptyComponent)
	}
	return nbt.String(data)
}

// textJSONValue is the JSON value of a component tag; a byte is a flag.
func textJSONValue(tag nbt.Tag) any {
	switch v := tag.(type) {
	case nbt.String:
		return string(v)
	case nbt.Byte:
		return v != 0
	case nbt.Short:
		return int64(v)
	case nbt.Int:
		return int64(v)
	case nbt.Long:
		return int64(v)
	case nbt.Float:
		return float64(v)
	case nbt.Double:
		return float64(v)
	case nbt.List:
		out := make([]any, len(v.Items))
		for i, item := range v.Items {
			out[i] = textJSONValue(nbt.ListElement(item))
		}
		return out
	}
	if c, ok := nbt.CompoundView(tag); ok {
		out := make(map[string]any, len(c))
		for k, item := range c {
			out[k] = textJSONValue(nbt.ListElement(item))
		}
		return out
	}
	return nil
}

// flattenedBlockEntityText returns the fields of a block entity to write over
// its entries so an older file's fixers read its text; nil when it has none.
func flattenedBlockEntityText(entries nbt.Compound) map[string]nbt.Tag {
	var out map[string]nbt.Tag
	set := func(key string, value nbt.Tag) {
		if out == nil {
			out = map[string]nbt.Tag{}
		}
		out[key] = value
	}
	if name, ok := entries["CustomName"]; ok {
		set("CustomName", flattenText(name))
	}
	for _, side := range []string{"front_text", "back_text"} {
		text, ok := nbt.Get[nbt.Compound](entries, side)
		if !ok {
			continue
		}
		copied := make(nbt.Compound, len(text))
		maps.Copy(copied, text)
		for _, key := range []string{"messages", "filtered_messages"} {
			if list, ok := nbt.Get[nbt.List](text, key); ok {
				items := make([]nbt.Tag, len(list.Items))
				for i, item := range list.Items {
					items[i] = flattenText(item)
				}
				copied[key] = nbt.List{Elem: nbt.TypeString, Items: items}
			}
		}
		set(side, copied)
	}
	return out
}

// flattenBlockEntities returns the list's entries with their text as JSON
// strings. The entries are copied: a carried one is kept in memory as read.
func flattenBlockEntities(entries []nbt.Tag) []nbt.Tag {
	out := make([]nbt.Tag, len(entries))
	for i, entry := range entries {
		out[i] = entry
		view, ok := nbt.CompoundView(entry)
		if !ok {
			continue
		}
		fields := flattenedBlockEntityText(view)
		if fields == nil {
			continue
		}
		copied := make(nbt.Compound, len(view))
		maps.Copy(copied, view)
		maps.Copy(copied, fields)
		out[i] = copied
	}
	return out
}
