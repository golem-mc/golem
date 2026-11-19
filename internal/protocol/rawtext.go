package protocol

import (
	"maps"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// rawNBTDepth is how deep the client reads a tag: it caps NBT nesting at 512.
const rawNBTDepth = 512

// RawTextFits reports whether a component tag is within the size budget and
// the nesting depth the client reads, which a client that cannot decode it
// is taken down by.
func RawTextFits(tag nbt.Tag) bool {
	return textCharge(tag) <= textQuota && nbtDepth(tag, 1) <= rawNBTDepth
}

func nbtDepth(tag nbt.Tag, depth int) int {
	deepest := depth
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return nbtDepth(v.Entries(), depth)
	case nbt.Compound:
		for _, item := range v {
			deepest = max(deepest, nbtDepth(item, depth+1))
		}
	case nbt.List:
		for _, item := range v.Items {
			deepest = max(deepest, nbtDepth(listElementOf(item), depth+1))
		}
	}
	return deepest
}

// RawTextOf keeps a component tag whole where the Text model has no field for
// it, for a component a command typed. The caller has already run the tag
// through the component walker; false is a tag over the size or depth the
// client reads (RawTextFits).
// vanilla: ComponentSerialization.CODEC
func RawTextOf(tag nbt.Tag) (Text, bool) {
	if !RawTextFits(tag) {
		return Text{}, false
	}
	// A component the model holds is sent in its form, so the wire bytes of
	// one do not depend on how it was typed.
	if text, ok := StyledTextOf(tag); ok {
		return text, true
	}
	return Text{Raw: tag}, true
}

// ComponentTextOf reads a relayed component, preserving contents the Text
// model cannot hold. Command callers must also validate the full codec.
// vanilla: ComponentSerialization.CODEC
func ComponentTextOf(tag nbt.Tag) (Text, bool) {
	if text, ok := StyledTextOf(tag); ok {
		return text, true
	}
	if !IsTextComponent(tag) {
		return Text{}, false
	}
	return RawTextOf(tag)
}

// DialogRefValid validates an inline dialog or registry reference using the
// registry package's shared codec; nil refuses show_dialog click events.
var DialogRefValid func(nbt.Tag) bool

// StyledTextOf extends TextOf with unmodelled styles, kept as their tag:
// decorations, font, shadow_color, show_dialog clicks and false italic or
// underlined (which override an item's default style).
// vanilla: Style.Serializer.MAP_CODEC
func StyledTextOf(tag nbt.Tag) (Text, bool) {
	modelled, styled, ok := stripStyle(tag)
	if !ok {
		return Text{}, false
	}
	text, ok := TextOf(modelled)
	if !ok || !styled {
		return text, ok
	}
	if !RawTextFits(tag) {
		return Text{}, false
	}
	return Text{Raw: tag}, true
}

// stripStyle returns tag without the style keys the model has no field for,
// whether it held any, and false where one of them is a value the codec
// refuses. It walks the components a component nests: siblings, arguments
// and hover texts. It has no depth cap of its own: the NBT reader and
// RawTextFits bound the tree at the client's depth.
func stripStyle(tag nbt.Tag) (nbt.Tag, bool, bool) {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return stripStyle(v.Entries())
	case nbt.List:
		// items is allocated only once an element turns out styled.
		var items []nbt.Tag
		for i, item := range v.Items {
			inner := listElementOf(item)
			out, s, ok := stripStyle(inner)
			if !ok {
				return nil, false, false
			}
			if !s {
				if items != nil {
					items[i] = item
				}
				continue
			}
			if items == nil {
				items = make([]nbt.Tag, len(v.Items))
				copy(items, v.Items[:i])
			}
			if wrapper, ok := nbt.CompoundView(item); ok && len(wrapper) == 1 && wrapper[""] != nil {
				out = nbt.Compound{"": out}
			}
			items[i] = out
		}
		if items == nil {
			return tag, false, true
		}
		return nbt.List{Elem: v.Elem, Items: items}, true, true
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		var out nbt.Compound
		styled := false
		set := func(key string, value nbt.Tag, drop bool) {
			if out == nil {
				out = maps.Clone(compoundFields)
			}
			if drop {
				delete(out, key)
			} else {
				out[key] = value
			}
			styled = true
		}
		for key, value := range compoundFields {
			switch key {
			case "click_event":
				event, ok := nbt.CompoundView(value)
				if !ok || event["action"] != nbt.String("show_dialog") {
					continue
				}
				// vanilla: ClickEvent.ShowDialog.CODEC
				if !RawTextFits(event) || DialogRefValid == nil || !DialogRefValid(event["dialog"]) {
					return nil, false, false
				}
				set(key, nil, true)
			case "bold", "italic", "underlined", "strikethrough", "obfuscated":
				var flag bool
				if !booleanTag(value, &flag) {
					return nil, false, false
				}
				if (key != "italic" && key != "underlined") || !flag {
					set(key, nil, true)
				}
			case "keybind":
				// The model has no keybind content; the tag is kept and the
				// rest validated as empty text.
				if _, ok := value.(nbt.String); !ok {
					return nil, false, false
				}
				if _, has := compoundFields["text"]; has {
					continue
				}
				set(key, nil, true)
				set("text", nbt.String(""), false)
			case "font":
				font, ok := value.(nbt.String)
				if !ok || !ValidIdentifier(string(font)) {
					return nil, false, false
				}
				set(key, nil, true)
			case "shadow_color":
				if !validARGB(value) {
					return nil, false, false
				}
				set(key, nil, true)
			case "extra", "with":
				inner, s, ok := stripStyle(value)
				if !ok {
					return nil, false, false
				}
				if s {
					set(key, inner, false)
				}
			case "hover_event":
				event, ok := nbt.CompoundView(value)
				if !ok {
					continue
				}
				for _, field := range [...]string{"value", "name"} {
					nested, present := event[field]
					if !present {
						continue
					}
					inner, s, ok := stripStyle(nested)
					if !ok {
						return nil, false, false
					}
					if s {
						event = maps.Clone(event)
						event[field] = inner
						set(key, event, false)
					}
				}
			}
		}
		if !styled {
			return tag, false, true
		}
		return out, true, true
	}
	return tag, false, true
}

// validARGB reports whether a shadow colour is one the codec reads: an int,
// or a list of four floats.
// vanilla: ExtraCodecs.ARGB_COLOR_CODEC
func validARGB(value nbt.Tag) bool {
	switch v := value.(type) {
	case nbt.Int:
		return true
	case nbt.List:
		if len(v.Items) != 4 {
			return false
		}
		for _, item := range v.Items {
			if _, ok := listElementOf(item).(nbt.Float); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// FlagOn reads a flag the way a boolean codec does: any non-zero number is true.
func FlagOn(v nbt.Tag) bool {
	switch f := v.(type) {
	case nbt.Byte:
		return f != 0
	case nbt.Short:
		return f != 0
	case nbt.Int:
		return f != 0
	case nbt.Long:
		return f != 0
	case nbt.Float:
		return f != 0
	case nbt.Double:
		return f != 0
	}
	return false
}

// contentTypes maps a compound's explicit type to the field naming its content.
var contentTypes = map[string]string{
	"text": "text", "translatable": "translate", "score": "score",
	"selector": "selector", "keybind": "keybind", "nbt": "nbt",
}

// ComponentContent names the field that holds a component compound's
// content: text, translate, score, selector, keybind or nbt. Empty when the
// compound names none, or an explicit type that is not one of them.
func ComponentContent(c nbt.Compound) string {
	if t, ok := c["type"]; ok {
		name, ok := t.(nbt.String)
		if !ok {
			return ""
		}
		kind, ok := contentTypes[strings.TrimPrefix(string(name), "minecraft:")]
		if !ok {
			return ""
		}
		return kind
	}
	for _, field := range []string{"text", "translate", "score", "selector", "keybind", "nbt"} {
		if _, ok := c[field]; ok {
			return field
		}
	}
	return ""
}

// PlainTextOf renders a component tag or primitive translation argument,
// including its siblings, as Component.getString does for a name.
// vanilla: Component.getString, TranslatableContents.visit
func PlainTextOf(tag nbt.Tag) string {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return PlainTextOf(v.Entries())
	case nbt.Byte:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Short:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Int:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Long:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Float:
		return javaFloat(float32(v))
	case nbt.Double:
		return javaDouble(float64(v))
	case nbt.String:
		return string(v)
	case nbt.List:
		var sb strings.Builder
		for _, item := range v.Items {
			sb.WriteString(PlainTextOf(item))
		}
		return sb.String()
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		if inner, ok := compoundFields[""]; ok && len(compoundFields) == 1 {
			return PlainTextOf(inner) // a heterogeneous list's wrapper
		}
		var out string
		switch ComponentContent(compoundFields) {
		case "text":
			text, _ := nbt.Get[nbt.String](compoundFields, "text")
			out = string(text)
		case "translate":
			key, _ := nbt.Get[nbt.String](compoundFields, "translate")
			fallback, ok := nbt.Get[nbt.String](compoundFields, "fallback")
			if !ok {
				fallback = key
			}
			with, _ := nbt.Get[nbt.List](compoundFields, "with")
			args := make([]Text, len(with.Items))
			for i, item := range with.Items {
				item = listElementOf(item)
				arg, ok := textArgumentOf(item)
				if !ok {
					arg = Text{Raw: item}
				}
				args[i] = arg
			}
			w := textWriter{}
			w.template(translationTemplate(string(key), string(fallback)), args, style{})
			out = w.String()
		}
		if extra, ok := nbt.Get[nbt.List](compoundFields, "extra"); ok {
			out += PlainTextOf(extra)
		}
		return out
	}
	return ""
}
