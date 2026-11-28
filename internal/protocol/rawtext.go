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
	return StyledTextWithDialog(tag, DialogRefValid)
}

// StyledTextWithDialog reads styled text with the caller's dialog validator.
func StyledTextWithDialog(tag nbt.Tag, validDialog func(nbt.Tag) bool) (Text, bool) {
	return styledText(tag, validDialog, false)
}

// LenientTextOf reads a text component the way TagValueInput.read takes
// ComponentSerialization.CODEC's partial result: each unreadable style field
// is dropped while the rest of the component is kept, recursing into extra,
// with and hover components. Unreadable content still refuses the component.
// vanilla: ComponentSerialization.CODEC, Style.Serializer.MAP_CODEC, TagValueInput.read
func LenientTextOf(tag nbt.Tag) (Text, bool) {
	return LenientTextWithDialog(tag, DialogRefValid)
}

// LenientTextWithDialog is LenientTextOf with the caller's dialog validator.
func LenientTextWithDialog(tag nbt.Tag, validDialog func(nbt.Tag) bool) (Text, bool) {
	return styledText(tag, validDialog, true)
}

func styledText(tag nbt.Tag, validDialog func(nbt.Tag) bool, lenient bool) (Text, bool) {
	if tag == nil || (lenient && !RawTextFits(tag)) {
		return Text{}, false
	}
	modelled, cleaned, styled, _, ok := stripStyle(tag, validDialog, lenient)
	if !ok {
		return Text{}, false
	}
	text, ok := TextOf(modelled)
	if !ok || !styled {
		return text, ok
	}
	if !RawTextFits(cleaned) {
		return Text{}, false
	}
	return Text{Raw: cleaned}, true
}

// stripStyle walks a component tree (siblings, arguments and hover texts),
// stripping unmodelled-but-valid style keys from modelled so TextOf can check
// the rest. In lenient mode it also drops unreadable style fields from both
// modelled and cleaned, and accepts unmodelled content types (selector, score,
// nbt, object, translate fallback). It has no depth cap of its own: the NBT
// reader and RawTextFits bound the tree at the client's depth.
func stripStyle(tag nbt.Tag, validDialog func(nbt.Tag) bool, lenient bool) (modelled, cleaned nbt.Tag, styled, changed, ok bool) {
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return stripStyle(v.Entries(), validDialog, lenient)
	case nbt.List:
		// mItems / cItems are allocated only once an element changes.
		var mItems, cItems []nbt.Tag
		for i, item := range v.Items {
			inner := listElementOf(item)
			m, c, s, ch, ok := stripStyle(inner, validDialog, lenient)
			if !ok {
				return nil, nil, false, false, false
			}
			wrap := func(t nbt.Tag) nbt.Tag {
				if wrapper, ok := nbt.CompoundView(item); ok && len(wrapper) == 1 && wrapper[""] != nil {
					return nbt.Compound{"": t}
				}
				return t
			}
			if s || ch {
				if mItems == nil {
					mItems = make([]nbt.Tag, len(v.Items))
					copy(mItems, v.Items[:i])
				}
				mItems[i] = wrap(m)
				styled = styled || s
			} else if mItems != nil {
				mItems[i] = item
			}
			if ch {
				if cItems == nil {
					cItems = make([]nbt.Tag, len(v.Items))
					copy(cItems, v.Items[:i])
				}
				cItems[i] = wrap(c)
				changed = true
			} else if cItems != nil {
				cItems[i] = item
			}
		}
		modelled, cleaned = tag, tag
		if mItems != nil {
			modelled = nbt.List{Elem: v.Elem, Items: mItems}
		}
		if cItems != nil {
			cleaned = nbt.List{Elem: v.Elem, Items: cItems}
		}
		return modelled, cleaned, styled, changed, true
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		var modelledOut, cleanedOut nbt.Compound
		ensureModelled := func() {
			if modelledOut == nil {
				modelledOut = maps.Clone(compoundFields)
			}
		}
		ensureCleaned := func() {
			if cleanedOut == nil {
				cleanedOut = maps.Clone(compoundFields)
			}
		}
		strip := func(key string) {
			ensureModelled()
			delete(modelledOut, key)
			styled = true
		}
		setModelled := func(key string, value nbt.Tag) {
			ensureModelled()
			modelledOut[key] = value
			styled = true
		}
		drop := func(key string) {
			ensureModelled()
			delete(modelledOut, key)
			ensureCleaned()
			delete(cleanedOut, key)
			changed = true
		}
		update := func(key string, mVal, cVal nbt.Tag, s, ch bool) {
			if s || ch {
				ensureModelled()
				modelledOut[key] = mVal
				styled = styled || s
			}
			if ch {
				ensureCleaned()
				cleanedOut[key] = cVal
				changed = true
			}
		}
		for key, value := range compoundFields {
			switch key {
			case "click_event":
				event, isMap := nbt.CompoundView(value)
				if isMap && event["action"] == nbt.String("show_dialog") {
					// vanilla: ClickEvent.ShowDialog.CODEC
					if !RawTextFits(event) || validDialog == nil || !validDialog(event["dialog"]) {
						if lenient {
							drop(key)
							continue
						}
						return nil, nil, false, false, false
					}
					strip(key)
					continue
				}
				if lenient {
					if isMap && event["action"] == nbt.String("custom") {
						if id, ok := nbt.Get[nbt.String](event, "id"); ok && ValidIdentifier(string(id)) {
							strip(key)
							continue
						}
						drop(key)
						continue
					}
					var scratch Text
					if !parseClickEvent(value, &scratch) {
						drop(key)
					}
				}
			case "bold", "italic", "underlined", "strikethrough", "obfuscated":
				var flag bool
				if !booleanTag(value, &flag) {
					if lenient {
						drop(key)
						continue
					}
					return nil, nil, false, false, false
				}
				if (key != "italic" && key != "underlined") || !flag {
					strip(key)
				}
			case "keybind":
				// The model has no keybind content; the tag is kept and the
				// rest validated as empty text.
				if _, ok := value.(nbt.String); !ok {
					return nil, nil, false, false, false
				}
				if _, has := compoundFields["text"]; has {
					continue
				}
				strip(key)
				setModelled("text", nbt.String(""))
			case "font":
				font, ok := value.(nbt.String)
				if !ok || !ValidIdentifier(string(font)) {
					if lenient {
						drop(key)
						continue
					}
					return nil, nil, false, false, false
				}
				strip(key)
			case "shadow_color":
				if !validARGB(value) {
					if lenient {
						drop(key)
						continue
					}
					return nil, nil, false, false, false
				}
				strip(key)
			case "color":
				if lenient {
					if s, ok := value.(nbt.String); !ok || !validColor(string(s)) {
						drop(key)
					}
				}
			case "insertion":
				if lenient {
					if _, ok := value.(nbt.String); !ok {
						drop(key)
					}
				}
			case "extra", "with":
				m, c, s, ch, ok := stripStyle(value, validDialog, lenient)
				if !ok {
					return nil, nil, false, false, false
				}
				update(key, m, c, s, ch)
			case "hover_event":
				event, ok := nbt.CompoundView(value)
				if !ok {
					if lenient {
						drop(key)
					}
					continue
				}
				var mEvent, cEvent nbt.Compound
				eventStyled, eventChanged, nestedOK := false, false, true
				for _, field := range [...]string{"value", "name"} {
					nested, present := event[field]
					if !present {
						continue
					}
					m, c, s, ch, ok := stripStyle(nested, validDialog, lenient)
					if !ok {
						if !lenient {
							return nil, nil, false, false, false
						}
						nestedOK = false
						break
					}
					if s || ch {
						if mEvent == nil {
							mEvent = maps.Clone(event)
						}
						mEvent[field] = m
						eventStyled = eventStyled || s
					}
					if ch {
						if cEvent == nil {
							cEvent = maps.Clone(event)
						}
						cEvent[field] = c
						eventChanged = true
					}
				}
				if lenient {
					check := event
					if mEvent != nil {
						check = mEvent
					}
					var scratch Text
					if !nestedOK || !parseHoverEvent(check, &scratch) {
						drop(key)
						continue
					}
				}
				if mEvent == nil {
					mEvent = event
				}
				if cEvent == nil {
					cEvent = event
				}
				update(key, mEvent, cEvent, eventStyled, eventChanged)
			}
		}
		if lenient && !stripLenientContent(compoundFields, validDialog, strip, setModelled, drop, func(key string, val nbt.Tag) {
			ensureCleaned()
			cleanedOut[key] = val
			changed = true
		}) {
			return nil, nil, false, false, false
		}
		modelled, cleaned = tag, tag
		if modelledOut != nil {
			modelled = modelledOut
		}
		if cleanedOut != nil {
			cleaned = cleanedOut
		}
		return modelled, cleaned, styled, changed, true
	}
	return tag, tag, false, false, true
}

// stripLenientContent handles unmodelled content types (selector, score, nbt,
// object, translate fallback, explicit type) in lenient mode by stripping
// their keys from the modelled copy so TextOf can validate the rest.
func stripLenientContent(
	fields nbt.Compound,
	validDialog func(nbt.Tag) bool,
	strip func(string),
	setModelled func(string, nbt.Tag),
	drop func(string),
	setCleaned func(string, nbt.Tag),
) bool {
	if rawType, has := fields["type"]; has {
		s, ok := rawType.(nbt.String)
		if !ok {
			return false
		}
		kind := strings.TrimPrefix(string(s), "minecraft:")
		if _, known := contentTypes[kind]; !known && kind != "object" {
			return false
		}
		strip("type")
	}
	cleanSeparator := func() bool {
		sep, has := fields["separator"]
		if !has {
			return true
		}
		m, c, _, ch, ok := stripStyle(sep, validDialog, true)
		if !ok {
			return false
		}
		if _, ok := TextOf(m); !ok {
			return false
		}
		if ch {
			setCleaned("separator", c)
		}
		strip("separator")
		return true
	}
	switch ComponentContent(fields) {
	case "text", "keybind":
		return true
	case "translate":
		if _, ok := fields["translate"].(nbt.String); !ok {
			return false
		}
		// vanilla: TranslatableContents.MAP_CODEC (lenientOptionalFieldOf)
		if fb, has := fields["fallback"]; has {
			if _, ok := fb.(nbt.String); ok {
				strip("fallback")
			} else {
				drop("fallback")
			}
		}
		return true
	case "selector":
		if _, ok := fields["selector"].(nbt.String); !ok || !cleanSeparator() {
			return false
		}
		strip("selector")
		if _, has := fields["text"]; !has {
			setModelled("text", nbt.String(""))
		}
		return true
	case "score":
		score, ok := nbt.CompoundView(fields["score"])
		if !ok {
			return false
		}
		if _, ok := score["name"].(nbt.String); !ok {
			return false
		}
		if _, ok := score["objective"].(nbt.String); !ok {
			return false
		}
		strip("score")
		if _, has := fields["text"]; !has {
			setModelled("text", nbt.String(""))
		}
		return true
	case "nbt":
		if _, ok := fields["nbt"].(nbt.String); !ok || !cleanSeparator() {
			return false
		}
		for _, k := range [...]string{"nbt", "block", "entity", "storage", "interpret", "source"} {
			if _, has := fields[k]; has {
				strip(k)
			}
		}
		if _, has := fields["text"]; !has {
			setModelled("text", nbt.String(""))
		}
		return true
	default:
		if sprite, ok := fields["sprite"].(nbt.String); ok && ValidIdentifier(string(sprite)) {
			if atlas, has := fields["atlas"]; has {
				if s, ok := atlas.(nbt.String); !ok || !ValidIdentifier(string(s)) {
					return false
				}
			}
		} else if player, has := fields["player"]; has {
			if _, ok := nbt.CompoundView(player); !ok {
				return false
			}
		} else {
			return true
		}
		for _, k := range [...]string{"sprite", "atlas", "player", "object"} {
			if _, has := fields[k]; has {
				strip(k)
			}
		}
		if _, has := fields["text"]; !has {
			setModelled("text", nbt.String(""))
		}
		return true
	}
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
