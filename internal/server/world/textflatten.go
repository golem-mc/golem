package world

import (
	"encoding/json"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// unflattenMessages converts a message list's JSON strings to components.
func unflattenMessages(list nbt.List) nbt.List {
	items := make([]nbt.Tag, len(list.Items))
	for i, item := range list.Items {
		items[i] = unflattenJSON(nbt.ListElement(item))
	}
	return mixedList(items)
}

// unflattenBlockEntityText applies UnflattenTextComponentFix to a block entity:
// sign messages and a CustomName.
func unflattenBlockEntityText(entries nbt.Compound) {
	if name, ok := entries["CustomName"]; ok {
		entries["CustomName"] = unflattenJSON(name)
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

// fixSignClickEvents renames the click and hover events in a sign's lines.
func fixSignClickEvents(entries nbt.Compound) {
	for _, side := range []string{"front_text", "back_text"} {
		text, ok := nbt.Get[nbt.Compound](entries, side)
		if !ok {
			continue
		}
		for _, key := range []string{"messages", "filtered_messages"} {
			if list, ok := nbt.Get[nbt.List](text, key); ok {
				for i, line := range list.Items {
					list.Items[i] = fixListItemEvents(line)
				}
			}
		}
	}
}

// fixTextEvents renames hoverEvent and clickEvent in a component and in the
// components under it, and returns the component.
// vanilla: TextComponentHoverAndClickEventFix
func fixTextEvents(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case nbt.List:
		for i, item := range v.Items {
			v.Items[i] = fixListItemEvents(item)
		}
		return v
	case nbt.Compound:
		if extra, ok := nbt.Get[nbt.List](v, "extra"); ok {
			fixTextEvents(extra)
		}
		if sep, ok := v["separator"]; ok {
			v["separator"] = fixTextEvents(sep)
		}
		if hover, ok := nbt.Get[nbt.Compound](v, "hoverEvent"); ok {
			delete(v, "hoverEvent")
			v["hover_event"] = fixHoverEvent(hover)
		}
		if click, ok := nbt.Get[nbt.Compound](v, "clickEvent"); ok {
			delete(v, "clickEvent")
			if fixed := fixClickEvent(click); fixed != nil {
				v["click_event"] = fixed
			}
		}
		return v
	}
	return tag
}

// fixListItemEvents is fixTextEvents for a list item, which a mixed list
// wraps in a compound with one empty key.
func fixListItemEvents(item nbt.Tag) nbt.Tag {
	if wrapper, ok := item.(nbt.Compound); ok && len(wrapper) == 1 {
		if inner, ok := wrapper[""]; ok {
			wrapper[""] = fixTextEvents(inner)
			return wrapper
		}
	}
	return fixTextEvents(item)
}

func fixHoverEvent(hover nbt.Compound) nbt.Compound {
	action, _ := nbt.Get[nbt.String](hover, "action")
	contents, _ := nbt.Get[nbt.Compound](hover, "contents")
	copyFrom := func(fields ...string) {
		delete(hover, "contents")
		for _, f := range fields {
			if value, ok := contents[f]; ok {
				hover[f] = value
			}
		}
	}
	switch action {
	case "show_text":
		renameField(hover, "contents", "value")
		if value, ok := hover["value"]; ok {
			hover["value"] = fixTextEvents(value)
		}
	case "show_item":
		copyFrom("id", "count", "components")
	case "show_entity":
		copyFrom("id", "type", "name")
		renameField(hover, "id", "uuid")
		renameField(hover, "type", "id")
		if name, ok := hover["name"]; ok {
			hover["name"] = fixTextEvents(name)
		}
	}
	return hover
}

// fixClickEvent renames a click event's value by its action; nil drops an
// event vanilla finds invalid.
func fixClickEvent(click nbt.Compound) nbt.Compound {
	action, _ := nbt.Get[nbt.String](click, "action")
	value, _ := click["value"].(nbt.String)
	switch action {
	case "open_url":
		if u, err := url.Parse(string(value)); err != nil || u.Scheme == "" || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
			return nil
		}
		renameField(click, "value", "url")
	case "open_file":
		renameField(click, "value", "path")
	case "run_command", "suggest_command":
		if strings.ContainsFunc(string(value), func(r rune) bool { return r == '§' || r < ' ' || r == 0x7f }) {
			return nil
		}
		renameField(click, "value", "command")
	case "change_page":
		raw, ok := click["value"]
		if !ok {
			return nil
		}
		page, ok := nbt.IntValue(raw)
		if s, isStr := raw.(nbt.String); isStr {
			n, err := strconv.Atoi(string(s))
			page, ok = int32(n), err == nil
		}
		if !ok {
			return nil
		}
		delete(click, "value")
		click["page"] = nbt.Int(max(page, 1))
	}
	return click
}
