package protocol

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// codecResult is a DataResult: err is empty on success; partial says a failed
// result still carries a value, which an either prefers to wrap-and-combine.
type codecResult struct {
	err     string
	partial bool
}

// errorModel reads a component tag the way ComponentSerialization.CODEC does
// to find its error message. It follows DFU's rules: a record joins its
// fields' messages in reverse declaration order, and an either returns the
// first side that has a partial value, else both messages.
type errorModel struct {
	// unmodelled is set when the tag reaches a codec this model has no
	// messages for, so the result is no use.
	unmodelled bool
}

// ComponentError is the message vanilla's component codec gives for a tag it
// refuses, or "" when the tag decodes or the refusal is one this model has no
// message for. It never accepts what TextOf refuses: callers fall back to
// their own message on "".
// vanilla: ComponentSerialization.CODEC
func ComponentError(tag nbt.Tag) string {
	if tag == nil {
		return ""
	}
	var m errorModel
	res := m.component(tag, 0)
	if m.unmodelled {
		return ""
	}
	return res.err
}

// StyleError is the message of Style.Serializer.CODEC for a tag it refuses,
// "" under the same terms as ComponentError.
// vanilla: Style.Serializer.CODEC
func StyleError(tag nbt.Tag) string {
	if tag == nil {
		return ""
	}
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	var m errorModel
	errs := m.style(fields, 0, nil)
	if m.unmodelled {
		return ""
	}
	return strings.Join(errs, "; ")
}

func eitherError(first, second string) string {
	return "Failed to parse either. First: " + first + "; Second: " + second
}

func (m *errorModel) component(tag nbt.Tag, depth int) codecResult {
	if depth > maxTextNesting {
		m.unmodelled = true
		return codecResult{}
	}
	tag = listElementOf(tag)
	// First side: either(string, non-empty list).
	var special codecResult
	switch v := tag.(type) {
	case nbt.String:
		return codecResult{}
	case nbt.List:
		list := m.list(v, depth)
		if list.err == "" {
			return codecResult{}
		}
		if list.partial {
			return list
		}
		special = codecResult{err: eitherError("Not a string", list.err)}
	default:
		special = codecResult{err: eitherError("Not a string", "Not a list: "+nbt.Stringify(tag))}
	}
	var full codecResult
	if fields, ok := nbt.CompoundView(tag); ok {
		full = m.record(fields, depth)
	} else {
		full = codecResult{err: "Not a map: " + nbt.Stringify(tag)}
	}
	if full.err == "" || full.partial {
		return full
	}
	return codecResult{err: eitherError(special.err, full.err)}
}

// list is nonEmptyList(listOf(component)): the failed elements' messages in
// reverse, and a refusal without a partial value when no element decoded.
func (m *errorModel) list(list nbt.List, depth int) codecResult {
	var errs []string
	decoded := 0
	for _, v := range slices.Backward(list.Items) {
		res := m.component(v, depth+1)
		if res.err != "" {
			errs = append(errs, res.err)
		}
		if res.err == "" || res.partial {
			decoded++
		}
	}
	if decoded == 0 {
		return codecResult{err: strings.Join(append(errs, "List must have contents"), "; ")}
	}
	return codecResult{err: strings.Join(errs, "; "), partial: len(errs) > 0}
}

// record is the component's record codec: contents, extra, then the style.
func (m *errorModel) record(fields nbt.Compound, depth int) codecResult {
	var errs []string
	errs = m.style(fields, depth, errs)
	if extra, ok := fields["extra"]; ok {
		if list, ok := extra.(nbt.List); !ok {
			errs = append(errs, "Not a list: "+nbt.Stringify(extra))
		} else if res := m.list(list, depth); res.err != "" {
			errs = append(errs, res.err)
		}
	}
	if !m.contents(fields, depth) {
		failure := "No matching codec found"
		if id, ok := fields["type"].(nbt.String); ok {
			failure = "Unknown element id: " + string(id)
		}
		errs = append(errs, failure)
		return codecResult{err: strings.Join(errs, "; ")}
	}
	return codecResult{err: strings.Join(errs, "; "), partial: len(errs) > 0}
}

// contents reports whether one of the content codecs reads the compound.
func (m *errorModel) contents(fields nbt.Compound, depth int) bool {
	if kind, ok := fields["type"]; ok {
		// A discriminator, which the typed codec reads in place of the
		// fuzzy search; only an unknown name has a message modelled here.
		id, isString := kind.(nbt.String)
		switch string(id) {
		case "text", "translatable", "keybind", "score", "selector", "nbt", "object":
			m.unmodelled = true
			return true
		}
		if !isString {
			m.unmodelled = true
			return true
		}
		return false
	}
	if _, ok := fields["text"].(nbt.String); ok {
		return true
	}
	if _, ok := fields["keybind"].(nbt.String); ok {
		return true
	}
	if _, ok := fields["translate"].(nbt.String); ok {
		// A non-string fallback is dropped, not an error.
		// vanilla: TranslatableContents.MAP_CODEC (lenientOptionalFieldOf)
		with, has := fields["with"]
		if !has {
			return true
		}
		list, ok := with.(nbt.List)
		if !ok {
			return false
		}
		for _, arg := range list.Items {
			arg = listElementOf(arg)
			if _, ok := nbt.NumberValue(arg); ok {
				continue
			}
			if m.component(arg, depth+1).err != "" {
				return false
			}
		}
		return true
	}
	for _, key := range []string{"selector", "score", "nbt", "object"} {
		if _, ok := fields[key]; ok {
			m.unmodelled = true
			return true
		}
	}
	return false
}

// style appends the style fields' messages, last declared first.
// vanilla: Style.Serializer.MAP_CODEC
func (m *errorModel) style(fields nbt.Compound, depth int, errs []string) []string {
	add := func(msg string) {
		if msg != "" {
			errs = append(errs, msg)
		}
	}
	if font, ok := fields["font"]; ok {
		add(stringCodec(font, IdentifierError))
	}
	if insertion, ok := fields["insertion"]; ok {
		add(stringCodec(insertion, nil))
	}
	if hover, ok := fields["hover_event"]; ok {
		add(m.event(hover, depth, true))
	}
	if click, ok := fields["click_event"]; ok {
		add(m.event(click, depth, false))
	}
	for _, key := range []string{"obfuscated", "strikethrough", "underlined", "italic", "bold"} {
		if flag, ok := fields[key]; ok {
			if _, ok := nbt.NumberValue(flag); !ok {
				add("Not a number")
			}
		}
	}
	if shadow, ok := fields["shadow_color"]; ok {
		add(m.shadowColor(shadow))
	}
	if color, ok := fields["color"]; ok {
		add(stringCodec(color, colorError))
	}
	return errs
}

// stringCodec is Codec.STRING with a validation: the refusal for a
// non-string tag, else check's message for the value.
func stringCodec(tag nbt.Tag, check func(string) string) string {
	s, ok := tag.(nbt.String)
	if !ok {
		return "Not a string"
	}
	if check == nil {
		return ""
	}
	return check(string(s))
}

// vanilla: TextColor.parseColor
func colorError(color string) string {
	if strings.HasPrefix(color, "#") {
		n, err := strconv.ParseInt(color[1:], 16, 32)
		if err != nil {
			return "Invalid color value: " + color
		}
		if n < 0 || n > 0xFFFFFF {
			return "Color value out of range: " + color
		}
		return ""
	}
	if colorNames[color] {
		return ""
	}
	return "Invalid color name: " + color
}

// vanilla: ExtraCodecs.ARGB_COLOR_CODEC
func (m *errorModel) shadowColor(tag nbt.Tag) string {
	if _, ok := nbt.NumberValue(tag); ok {
		return ""
	}
	switch v := tag.(type) {
	case nbt.List:
		if len(v.Items) != 4 {
			return eitherError("Not a number", "Input is not a list of 4 elements")
		}
		for _, item := range v.Items {
			if _, ok := nbt.NumberValue(listElementOf(item)); !ok {
				m.unmodelled = true
			}
		}
		return ""
	case nbt.IntArray:
		m.unmodelled = true
		return ""
	}
	return eitherError("Not a number", "Not a list: "+nbt.Stringify(tag))
}

// IdentifierError is Identifier.CODEC's message for a string, "" when valid.
// vanilla: Identifier.CODEC, Identifier.parse
func IdentifierError(id string) string {
	if ValidIdentifier(id) {
		return ""
	}
	namespace, path := "minecraft", id
	if i := strings.IndexByte(id, ':'); i >= 0 {
		path = id[i+1:]
		if i > 0 {
			namespace = id[:i]
		}
	}
	location := namespace + ":" + path
	if !identifierPart(namespace, "_.-") || namespace == ".." {
		return "Not a valid resource location: " + id +
			" Non [a-z0-9_.-] character in namespace of identifier: " + location
	}
	return "Not a valid resource location: " + id +
		" Non [a-z0-9/._-] character in path of location: " + location
}

func identifierPart(s, punctuation string) bool {
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune(punctuation, c) {
			continue
		}
		return false
	}
	return true
}

// event is a click or hover event's dispatch record.
// vanilla: ClickEvent.CODEC, HoverEvent.CODEC
func (m *errorModel) event(tag nbt.Tag, depth int, hover bool) string {
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	missing := func(key string) string { return "No key " + key + " in MapLike[" + nbt.Stringify(tag) + "]" }
	actionTag, ok := fields["action"]
	if !ok {
		return missing("action")
	}
	actionName, ok := actionTag.(nbt.String)
	if !ok {
		return "Not a string"
	}
	action := string(actionName)
	field := func(key string, check func(nbt.Tag) string) string {
		value, ok := fields[key]
		if !ok {
			return missing(key)
		}
		return check(value)
	}
	text := func(check func(string) string) func(nbt.Tag) string {
		return func(value nbt.Tag) string { return stringCodec(value, check) }
	}
	if hover {
		switch action {
		case "show_text":
			return field("value", func(value nbt.Tag) string { return m.component(value, depth+1).err })
		case "show_entity":
			var errs []string
			if name, ok := fields["name"]; ok {
				if msg := m.component(name, depth+1).err; msg != "" {
					errs = append(errs, msg)
				}
			}
			if msg := field("uuid", m.hoverUUID); msg != "" {
				errs = append(errs, msg)
			}
			if msg := field("id", text(func(id string) string {
				if msg := IdentifierError(id); msg != "" {
					return msg
				}
				qualified := id
				if !strings.Contains(id, ":") {
					qualified = "minecraft:" + id
				} else if strings.HasPrefix(id, ":") {
					qualified = "minecraft" + id
				}
				if !entityTypeNames[qualified] {
					return "Unknown registry key in ResourceKey[minecraft:root / minecraft:entity_type]: " + qualified
				}
				return ""
			})); msg != "" {
				errs = append(errs, msg)
			}
			return strings.Join(errs, "; ")
		case "show_item":
			m.unmodelled = true
			return ""
		}
		return "Unknown element name:" + action
	}
	switch action {
	case "open_url":
		return field("url", text(func(url string) string {
			if ValidUntrustedURI(url) {
				return ""
			}
			if strings.ContainsRune(url, ':') || strings.IndexFunc(url, func(c rune) bool { return c > 0x7e || !isURIChar(byte(c)) }) >= 0 {
				m.unmodelled = true
				return ""
			}
			return "Missing protocol in URI: " + url + ": " + url
		}))
	case "run_command", "suggest_command":
		return field("command", text(func(command string) string {
			for _, c := range command {
				if c == 0xA7 || c < ' ' || c == 0x7F {
					return "Disallowed chat character: '" + string(c) + "'"
				}
			}
			return ""
		}))
	case "copy_to_clipboard":
		return field("value", text(nil))
	case "change_page":
		return field("page", func(page nbt.Tag) string {
			n, ok := nbt.IntValue(page)
			if !ok {
				return "Not a number"
			}
			if n < 1 {
				return "Value must be positive: " + strconv.Itoa(int(n))
			}
			return ""
		})
	case "open_file":
		return "Click event type not allowed: OPEN_FILE"
	case "show_dialog", "custom":
		m.unmodelled = true
		return ""
	}
	return "Unknown element name:" + action
}

// hoverUUID is UUIDUtil.LENIENT_CODEC: four ints or a string.
func (m *errorModel) hoverUUID(tag nbt.Tag) string {
	if str, ok := tag.(nbt.String); ok {
		if _, reason := parseJavaUUID(string(str)); reason != "" {
			return eitherError("Not a list", "Invalid UUID "+string(str)+": "+reason)
		}
		return ""
	}
	switch tag.(type) {
	case nbt.IntArray, nbt.List:
		if words, ok := nbt.UUIDWords(tag); ok && len(words) == 4 {
			return ""
		}
		if list, ok := tag.(nbt.List); ok && len(list.Items) == 4 {
			m.unmodelled = true
			return ""
		}
		return eitherError("Input is not a list of 4 ints", "Not a string")
	}
	return eitherError("Not a list", "Not a string")
}
