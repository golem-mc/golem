package protocol

import (
	"maps"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// LenientTextOf reads a text component the way TagValueInput.read takes
// ComponentSerialization.CODEC's partial result: each unreadable style field
// is dropped while the rest of the component is kept, recursing into extra
// and with. Unreadable content still refuses the whole component.
// vanilla: ComponentSerialization.CODEC, Style.Serializer.MAP_CODEC, TagValueInput.read
func LenientTextOf(tag nbt.Tag) (Text, bool) {
	return LenientTextWithDialog(tag, DialogRefValid)
}

// LenientTextWithDialog is LenientTextOf with the caller's dialog validator.
func LenientTextWithDialog(tag nbt.Tag, validDialog func(nbt.Tag) bool) (Text, bool) {
	if tag == nil || !RawTextFits(tag) {
		return Text{}, false
	}
	cleaned, _, ok := cleanLenientComponent(tag, validDialog)
	if !ok {
		return Text{}, false
	}
	if text, ok := StyledTextWithDialog(cleaned, validDialog); ok {
		return text, true
	}
	// Preserve valid components whose content or events are outside the
	// modelled subset (selector, score, nbt, object, translate fallback,
	// show_item, custom click).
	if !RawTextFits(cleaned) {
		return Text{}, false
	}
	return Text{Raw: cleaned}, true
}

// cleanLenientComponent drops unreadable style fields from a component tag,
// returning the cleaned tag, whether it changed, and false when the content
// itself cannot be read.
func cleanLenientComponent(tag nbt.Tag, validDialog func(nbt.Tag) bool) (nbt.Tag, bool, bool) {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return cleanLenientComponent(v.Entries(), validDialog)
	case nbt.String:
		return v, false, true
	case nbt.List:
		return cleanLenientList(v, validDialog)
	case nbt.CompoundTag:
		return cleanLenientCompound(v.Entries(), validDialog)
	default:
		return nil, false, false
	}
}

func cleanLenientList(list nbt.List, validDialog func(nbt.Tag) bool) (nbt.Tag, bool, bool) {
	if len(list.Items) == 0 {
		return nil, false, false
	}
	var items []nbt.Tag
	for i, item := range list.Items {
		inner := listElementOf(item)
		cleaned, changed, ok := cleanLenientComponent(inner, validDialog)
		if !ok {
			return nil, false, false
		}
		if !changed {
			if items != nil {
				items[i] = inner
			}
			continue
		}
		if items == nil {
			items = make([]nbt.Tag, len(list.Items))
			for j := range i {
				items[j] = listElementOf(list.Items[j])
			}
		}
		items[i] = cleaned
	}
	if items == nil {
		return list, false, true
	}
	return nbt.NewList(items), true, true
}

func cleanLenientCompound(fields nbt.Compound, validDialog func(nbt.Tag) bool) (nbt.Tag, bool, bool) {
	var out nbt.Compound
	changed := false
	mutate := func() {
		if out == nil {
			out = maps.Clone(fields)
		}
		changed = true
	}
	set := func(key string, value nbt.Tag) {
		mutate()
		out[key] = value
	}
	drop := func(key string) {
		mutate()
		delete(out, key)
	}

	if !cleanLenientContent(fields, validDialog, set, drop) {
		return nil, false, false
	}

	if extra, has := fields["extra"]; has {
		list, ok := extra.(nbt.List)
		if !ok {
			return nil, false, false
		}
		cleaned, ch, ok := cleanLenientList(list, validDialog)
		if !ok {
			return nil, false, false
		}
		if ch {
			set("extra", cleaned)
		}
	}

	// Style fields: drop each unreadable field rather than refusing the text.
	// vanilla: Style.Serializer.MAP_CODEC
	if color, has := fields["color"]; has {
		if s, ok := color.(nbt.String); !ok || !validColor(string(s)) {
			drop("color")
		}
	}
	if shadow, has := fields["shadow_color"]; has {
		if !validARGB(shadow) {
			drop("shadow_color")
		}
	}
	for _, key := range [...]string{"bold", "italic", "underlined", "strikethrough", "obfuscated"} {
		if flag, has := fields[key]; has {
			if _, ok := nbt.NumberValue(flag); !ok {
				drop(key)
			}
		}
	}
	if insertion, has := fields["insertion"]; has {
		if _, ok := insertion.(nbt.String); !ok {
			drop("insertion")
		}
	}
	if font, has := fields["font"]; has {
		if s, ok := font.(nbt.String); !ok || !ValidIdentifier(string(s)) {
			drop("font")
		}
	}
	if click, has := fields["click_event"]; has {
		if !validLenientClickEvent(click, validDialog) {
			drop("click_event")
		}
	}
	if hover, has := fields["hover_event"]; has {
		cleaned, ch, ok := cleanLenientHoverEvent(hover, validDialog)
		if !ok {
			drop("hover_event")
		} else if ch {
			set("hover_event", cleaned)
		}
	}

	if !changed {
		return fields, false, true
	}
	return out, true, true
}

// cleanLenientContent validates the compound's content fields and cleans any
// nested components in with or separator.
// vanilla: ComponentSerialization.createCodec
func cleanLenientContent(fields nbt.Compound, validDialog func(nbt.Tag) bool, set func(string, nbt.Tag), drop func(string)) bool {
	if kind, has := fields["type"]; has {
		s, ok := kind.(nbt.String)
		if !ok {
			return false
		}
		switch strings.TrimPrefix(string(s), "minecraft:") {
		case "text":
			_, ok := fields["text"].(nbt.String)
			return ok
		case "translatable":
			return cleanLenientTranslate(fields, validDialog, set, drop)
		case "keybind":
			_, ok := fields["keybind"].(nbt.String)
			return ok
		case "score":
			return validScoreContent(fields["score"])
		case "selector":
			return cleanLenientSelector(fields, validDialog, set)
		case "nbt":
			return cleanLenientNBT(fields, validDialog, set)
		case "object":
			return validObjectContent(fields)
		default:
			return false
		}
	}
	if _, ok := fields["text"].(nbt.String); ok {
		return true
	}
	if _, ok := fields["translate"].(nbt.String); ok {
		return cleanLenientTranslate(fields, validDialog, set, drop)
	}
	if _, ok := fields["keybind"].(nbt.String); ok {
		return true
	}
	if score, has := fields["score"]; has && validScoreContent(score) {
		return true
	}
	if _, ok := fields["selector"].(nbt.String); ok {
		return cleanLenientSelector(fields, validDialog, set)
	}
	if _, ok := fields["nbt"].(nbt.String); ok {
		return cleanLenientNBT(fields, validDialog, set)
	}
	if validObjectContent(fields) {
		return true
	}
	return false
}

func cleanLenientTranslate(fields nbt.Compound, validDialog func(nbt.Tag) bool, set func(string, nbt.Tag), drop func(string)) bool {
	if _, ok := fields["translate"].(nbt.String); !ok {
		return false
	}
	// vanilla: TranslatableContents.MAP_CODEC (lenientOptionalFieldOf)
	if fb, has := fields["fallback"]; has {
		if _, ok := fb.(nbt.String); !ok {
			drop("fallback")
		}
	}
	with, has := fields["with"]
	if !has {
		return true
	}
	list, ok := with.(nbt.List)
	if !ok {
		return false
	}
	var items []nbt.Tag
	for i, item := range list.Items {
		arg := listElementOf(item)
		if _, ok := nbt.NumberValue(arg); ok {
			if items != nil {
				items[i] = arg
			}
			continue
		}
		cleaned, changed, ok := cleanLenientComponent(arg, validDialog)
		if !ok {
			return false
		}
		if !changed {
			if items != nil {
				items[i] = arg
			}
			continue
		}
		if items == nil {
			items = make([]nbt.Tag, len(list.Items))
			for j := range i {
				items[j] = listElementOf(list.Items[j])
			}
		}
		items[i] = cleaned
	}
	if items != nil {
		set("with", nbt.NewList(items))
	}
	return true
}

func validScoreContent(tag nbt.Tag) bool {
	score, ok := nbt.CompoundView(tag)
	if !ok {
		return false
	}
	_, hasName := score["name"].(nbt.String)
	_, hasObj := score["objective"].(nbt.String)
	return hasName && hasObj
}

func cleanLenientSelector(fields nbt.Compound, validDialog func(nbt.Tag) bool, set func(string, nbt.Tag)) bool {
	if _, ok := fields["selector"].(nbt.String); !ok {
		return false
	}
	if sep, has := fields["separator"]; has {
		cleaned, changed, ok := cleanLenientComponent(sep, validDialog)
		if !ok {
			return false
		}
		if changed {
			set("separator", cleaned)
		}
	}
	return true
}

func cleanLenientNBT(fields nbt.Compound, validDialog func(nbt.Tag) bool, set func(string, nbt.Tag)) bool {
	if _, ok := fields["nbt"].(nbt.String); !ok {
		return false
	}
	if sep, has := fields["separator"]; has {
		cleaned, changed, ok := cleanLenientComponent(sep, validDialog)
		if !ok {
			return false
		}
		if changed {
			set("separator", cleaned)
		}
	}
	return true
}

func validObjectContent(fields nbt.Compound) bool {
	if sprite, ok := fields["sprite"].(nbt.String); ok && ValidIdentifier(string(sprite)) {
		if atlas, has := fields["atlas"]; has {
			if s, ok := atlas.(nbt.String); !ok || !ValidIdentifier(string(s)) {
				return false
			}
		}
		return true
	}
	if player, has := fields["player"]; has {
		_, ok := nbt.CompoundView(player)
		return ok
	}
	return false
}

// validLenientClickEvent reports whether a click_event tag is accepted by
// ClickEvent.CODEC.
// vanilla: ClickEvent.CODEC
func validLenientClickEvent(tag nbt.Tag, validDialog func(nbt.Tag) bool) bool {
	event, ok := nbt.CompoundView(tag)
	if !ok {
		return false
	}
	action, ok := nbt.Get[nbt.String](event, "action")
	if !ok {
		return false
	}
	switch string(action) {
	case "suggest_command", "run_command":
		cmd, ok := nbt.Get[nbt.String](event, "command")
		return ok && ValidChatString(string(cmd))
	case "copy_to_clipboard":
		_, ok := nbt.Get[nbt.String](event, "value")
		return ok
	case "open_url":
		u, ok := nbt.Get[nbt.String](event, "url")
		return ok && ValidUntrustedURI(string(u))
	case "change_page":
		page, ok := nbt.IntValue(event["page"])
		return ok && page >= 1
	case "show_dialog":
		return RawTextFits(event) && validDialog != nil && validDialog(event["dialog"])
	case "custom":
		id, ok := nbt.Get[nbt.String](event, "id")
		return ok && ValidIdentifier(string(id))
	default:
		return false
	}
}

// cleanLenientHoverEvent validates a hover_event tag and cleans any nested
// components inside it; false when the event itself is unreadable.
// vanilla: HoverEvent.CODEC
func cleanLenientHoverEvent(tag nbt.Tag, validDialog func(nbt.Tag) bool) (nbt.Tag, bool, bool) {
	event, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, false, false
	}
	action, ok := nbt.Get[nbt.String](event, "action")
	if !ok {
		return nil, false, false
	}
	switch string(action) {
	case "show_text":
		val, has := event["value"]
		if !has {
			return nil, false, false
		}
		cleaned, changed, ok := cleanLenientComponent(val, validDialog)
		if !ok {
			return nil, false, false
		}
		if !changed {
			return event, false, true
		}
		out := maps.Clone(event)
		out["value"] = cleaned
		return out, true, true
	case "show_entity":
		id, ok := nbt.Get[nbt.String](event, "id")
		if !ok || !ValidIdentifier(string(id)) {
			return nil, false, false
		}
		qualified := string(id)
		if !strings.Contains(qualified, ":") {
			qualified = "minecraft:" + qualified
		} else if strings.HasPrefix(qualified, ":") {
			qualified = "minecraft" + qualified
		}
		if !entityTypeNames[qualified] {
			return nil, false, false
		}
		if _, ok := hoverEntityUUID(event["uuid"]); !ok {
			return nil, false, false
		}
		if name, has := event["name"]; has {
			cleaned, changed, ok := cleanLenientComponent(name, validDialog)
			if !ok {
				out := maps.Clone(event)
				delete(out, "name")
				return out, true, true
			}
			if changed {
				out := maps.Clone(event)
				out["name"] = cleaned
				return out, true, true
			}
		}
		return event, false, true
	case "show_item":
		id, ok := nbt.Get[nbt.String](event, "id")
		if !ok || !ValidIdentifier(string(id)) {
			return nil, false, false
		}
		if count, has := event["count"]; has {
			n, ok := nbt.IntValue(count)
			if !ok || n < 1 {
				return nil, false, false
			}
		}
		if comps, has := event["components"]; has {
			if _, ok := nbt.CompoundView(comps); !ok {
				return nil, false, false
			}
		}
		return event, false, true
	default:
		return nil, false, false
	}
}
