package registry

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func init() {
	protocol.DialogRefValid = func(tag nbt.Tag) bool { return DialogRefMalformed(tag) == "" }
}

// DialogMalformed reports inline-dialog codec refusals in codec field order.
// vanilla: Dialog.DIRECT_CODEC, CommonDialogData.MAP_CODEC
func DialogMalformed(c nbt.Compound) string {
	msg, _ := dialogDiagnostic(c)
	return msg
}

func dialogDiagnostic(c nbt.Compound) (string, bool) {
	if msg := textFieldMalformed(c, "type", func(tag nbt.Tag) string {
		return textRegistryMalformed(tag, "minecraft:dialog_type", func(name string) bool {
			return ValidDialogType(strings.TrimPrefix(name, "minecraft:"))
		})
	}); msg != "" {
		return msg, false
	}
	common, partial := dialogCommonDiagnostic(c)
	switch DialogKind(c) {
	case "notice":
		return JoinFieldErrors(optionalButton(c, "action"), common), partial
	case "confirmation":
		return JoinFieldErrors(requiredButton(c, "no"), requiredButton(c, "yes"), common),
			partial && buttonHasPartial(c["no"]) && buttonHasPartial(c["yes"])
	case "multi_action":
		msg, has := codecFieldDiagnostic(c, "actions", func(tag nbt.Tag) (string, bool) {
			return codecListDiagnostic(tag, buttonDiagnostic, true)
		})
		return JoinFieldErrors(dialogListTail(c, false), msg, common), partial && has
	case "server_links":
		return JoinFieldErrors(dialogListTail(c, true), common), partial
	case "dialog_list":
		msg, has := codecFieldDiagnostic(c, "dialogs", dialogHoldersDiagnostic)
		return JoinFieldErrors(dialogListTail(c, true), msg, common), partial && has
	}
	return common, partial
}

// DialogKind reads the inline dialog's unqualified type.
func DialogKind(c nbt.Compound) string {
	s, _ := c["type"].(nbt.String)
	return strings.TrimPrefix(Qualify(string(s)), "minecraft:")
}

func dialogCommonDiagnostic(c nbt.Compound) (string, bool) {
	after := "close"
	afterError := ""
	if v, ok := c["after_action"]; ok {
		afterError = textStringMalformed(v)
		if value, ok := v.(nbt.String); ok {
			after = string(value)
			if after != "close" && after != "none" && after != "wait_for_response" {
				afterError = "Unknown element name:" + after
			}
		}
	}
	title, partial := codecFieldDiagnostic(c, "title", textComponentDiagnostic)
	msg := JoinFieldErrors(dialogInputs(c), dialogBodies(c), afterError,
		optionalBool(c, "pause"), optionalBool(c, "can_close_with_escape"),
		optionalComponent(c, "external_title"), title)
	pause := true
	if b, ok := boolOf(c["pause"]); ok {
		pause = b
	}
	if partial && pause && after == "none" {
		return JoinFieldErrors(msg, "Dialogs that pause the game must use after_action values that unpause it after user action!"), false
	}
	return msg, partial
}

func dialogBodies(c nbt.Compound) string {
	if tag, ok := c["body"]; ok {
		msg, _ := compactListDiagnostic(tag, dialogBodyDiagnostic)
		return msg
	}
	return ""
}

// vanilla: DialogBody.DIALOG_BODY_CODEC, PlainMessage.MAP_CODEC, ItemBody.MAP_CODEC
func dialogBodyDiagnostic(tag nbt.Tag) (string, bool) {
	b, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag), false
	}
	if msg := textFieldMalformed(b, "type", func(tag nbt.Tag) string {
		return textRegistryMalformed(tag, "minecraft:dialog_body_type", func(name string) bool {
			return name == "minecraft:plain_message" || name == "minecraft:item"
		})
	}); msg != "" {
		return msg, false
	}
	if DialogKind(b) == "plain_message" {
		return plainMessageMapDiagnostic(b)
	}
	required, partial := codecFieldDiagnostic(b, "item", itemTemplateDiagnostic)
	description := ""
	if value, present := b["description"]; present {
		description = plainMessageCheck(value)
	}
	return JoinFieldErrors(optionalRange(b, "height", 1, 256), optionalRange(b, "width", 1, 256),
		optionalBool(b, "show_tooltip"), optionalBool(b, "show_decorations"), description, required), partial
}

// vanilla: PlainMessage.MAP_CODEC
func plainMessageMapDiagnostic(b nbt.Compound) (string, bool) {
	msg, partial := codecFieldDiagnostic(b, "contents", textComponentDiagnostic)
	return JoinFieldErrors(optionalRange(b, "width", 1, 1024), msg), partial
}

// vanilla: PlainMessage.CODEC
func plainMessageCheck(tag nbt.Tag) string {
	msg, _ := codecAlternative(tag, func(tag nbt.Tag) (string, bool) {
		b, ok := nbt.CompoundView(tag)
		if !ok {
			return "Not a map: " + nbt.Stringify(tag), false
		}
		return plainMessageMapDiagnostic(b)
	}, textComponentDiagnostic)
	return msg
}

// vanilla: Input.CODEC
func dialogInputs(c nbt.Compound) string {
	tag, ok := c["inputs"]
	if !ok {
		return ""
	}
	msg, _ := codecListDiagnostic(tag, func(tag nbt.Tag) (string, bool) {
		in, ok := nbt.CompoundView(tag)
		if !ok {
			return "Not a map: " + nbt.Stringify(tag), false
		}
		key := textFieldMalformed(in, "key", func(tag nbt.Tag) string {
			if msg := textStringMalformed(tag); msg != "" {
				return msg
			}
			s := string(tag.(nbt.String))
			if !validVariableName(s) {
				return s + " is not a valid input name"
			}
			return ""
		})
		control := inputControlCheck(in)
		return JoinFieldErrors(control, key), key == "" && control == ""
	}, false)
	return msg
}

// vanilla: HolderSetCodec, TagKey.hashedCodec
func dialogHoldersDiagnostic(tag nbt.Tag) (string, bool) {
	tagError := textStringMalformed(tag)
	if name, ok := tag.(nbt.String); ok {
		if after, ok0 := strings.CutPrefix(string(name), "#"); ok0 {
			id := after
			tagError = protocol.IdentifierError(id)
			if tagError == "" {
				id = Qualify(id)
				if !dialogTagExists(id) {
					return "Missing tag: '" + id + "' in '" + DialogRegistry + "'", false
				}
				return "", true
			}
		} else {
			tagError = "Not a tag id"
		}
	}
	msg, partial := compactListDiagnostic(tag, dialogRefDiagnostic)
	if msg == "" || partial {
		return msg, partial
	}
	return profileEitherMalformed(tagError, msg), false
}

func buttonHasPartial(tag nbt.Tag) bool {
	b, ok := nbt.CompoundView(tag)
	if !ok || b["label"] == nil {
		return false
	}
	_, partial := textComponentDiagnostic(b["label"])
	return partial
}

func buttonDiagnostic(tag nbt.Tag) (string, bool) { return buttonCheck(tag), buttonHasPartial(tag) }

func dialogTagExists(name string) bool {
	for _, r := range Tags {
		if r.Registry != DialogRegistry {
			continue
		}
		for _, t := range r.Tags {
			if t.Name == name {
				return true
			}
		}
	}
	return false
}

// DialogRefMalformed checks a registry id or inline dialog; tags require a dialogs list.
// vanilla: Dialog.CODEC
func DialogRefMalformed(item nbt.Tag) string {
	msg, _ := dialogRefDiagnostic(item)
	return msg
}

// vanilla: RegistryFileCodec.decode
func dialogRefDiagnostic(item nbt.Tag) (string, bool) {
	if id, ok := item.(nbt.String); ok && protocol.ValidIdentifier(string(id)) {
		reg, _ := Get(DialogRegistry)
		name := Qualify(string(id))
		if !slices.Contains(reg.Entries, name) {
			return "Failed to get element " + name + " from registry " + DialogRegistry, false
		}
		return "", true
	}
	fields, ok := nbt.CompoundView(item)
	if !ok {
		return "Not a map: " + nbt.Stringify(item), false
	}
	return dialogDiagnostic(fields)
}

// ValidDialogType reports whether a dialog type is registered.
func ValidDialogType(kind string) bool {
	_, ok := dialogTypes[kind]
	return ok
}

func dialogListTail(c nbt.Compound, buttonWidth bool) string {
	columns := ""
	if v, ok := c["columns"]; ok {
		columns = positiveIntMalformed(v)
	}
	width := ""
	if buttonWidth {
		width = optionalRange(c, "button_width", 1, 1024)
	}
	return JoinFieldErrors(width, columns, optionalButton(c, "exit_action"))
}

func positiveIntMalformed(tag nbt.Tag) string {
	n, ok := nbt.IntValue(tag)
	if !ok {
		return "Not a number"
	}
	if n < 1 {
		return "Value must be positive: " + strconv.Itoa(int(n))
	}
	return ""
}

func buttonCheck(tag nbt.Tag) string {
	b, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	actionError := ""
	if v, has := b["action"]; has {
		a, isCompound := nbt.CompoundView(v)
		if !isCompound {
			actionError = "Not a map: " + nbt.Stringify(v)
		} else if msg := textFieldMalformed(a, "type", func(tag nbt.Tag) string {
			return textRegistryMalformed(tag, "minecraft:dialog_action_type", func(name string) bool {
				_, known := actionTypes[strings.TrimPrefix(name, "minecraft:")]
				return known
			})
		}); msg != "" {
			actionError = msg
		} else {
			kind := strings.TrimPrefix(Qualify(string(a["type"].(nbt.String))), "minecraft:")
			actionError = actionPayloadCheck(kind, a)
		}
	}
	return JoinFieldErrors(actionError, optionalRange(b, "width", 1, 1024),
		optionalComponent(b, "tooltip"), requiredComponent(b, "label"))
}

// vanilla: ClickEvent.*.CODEC, CommandTemplate.MAP_CODEC, CustomAll.MAP_CODEC
func actionPayloadCheck(kind string, a nbt.Compound) string {
	switch kind {
	case "dynamic/run_command":
		return textFieldMalformed(a, "template", func(tag nbt.Tag) string {
			if msg := textStringMalformed(tag); msg != "" {
				return msg
			}
			s := string(tag.(nbt.String))
			if msg := stringTemplateError(s); msg != "" {
				return "Failed to parse template " + s + ": " + msg
			}
			return ""
		})
	case "dynamic/custom":
		additions := ""
		if tag, ok := a["additions"]; ok {
			if _, ok := nbt.CompoundView(tag); !ok {
				additions = "Not a compound tag: " + nbt.Stringify(tag)
			}
		}
		return JoinFieldErrors(additions, textFieldMalformed(a, "id", resourceLocationMalformed))
	default:
		return clickActionMalformed(kind, a)
	}
}

// vanilla: StringTemplate.fromString
func stringTemplateError(input string) string {
	start := 0
	index := strings.IndexByte(input, '$')
	for index != -1 {
		if index == len(input)-1 || input[index+1] != '(' {
			next := strings.IndexByte(input[index+1:], '$')
			if next == -1 {
				break
			}
			index += 1 + next
			continue
		}
		end := strings.IndexByte(input[index+1:], ')')
		if end == -1 {
			return "Unterminated macro variable"
		}
		end += index + 1
		if name := input[index+2 : end]; !validVariableName(name) {
			return "Invalid macro variable name '" + name + "'"
		}
		start = end + 1
		next := strings.IndexByte(input[start:], '$')
		if next == -1 {
			break
		}
		index = start + next
	}
	if start == 0 {
		return "No variables in macro"
	}
	return ""
}

// validVariableName reports whether every UTF-16 unit is a letter, digit or
// underscore; Java checks units, so a supplementary letter is refused.
// vanilla: StringTemplate.isValidVariableName
func validVariableName(name string) bool {
	for _, u := range utf16.Encode([]rune(name)) {
		if r := rune(u); r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func requiredButton(c nbt.Compound, key string) string {
	v, ok := c[key]
	if !ok {
		return "No key " + key + " in MapLike[" + nbt.Stringify(c) + "]"
	}
	return buttonCheck(v)
}

func optionalButton(c nbt.Compound, key string) string {
	if v, ok := c[key]; ok {
		return buttonCheck(v)
	}
	return ""
}

func requiredComponent(c nbt.Compound, key string) string {
	v, ok := c[key]
	if !ok {
		return "No key " + key + " in MapLike[" + nbt.Stringify(c) + "]"
	}
	return componentCheck(v)
}

func optionalComponent(c nbt.Compound, key string) string {
	if v, ok := c[key]; ok {
		return componentCheck(v)
	}
	return ""
}

// vanilla: ComponentSerialization.CODEC
func componentCheck(v nbt.Tag) string {
	return TextComponentMalformed(v)
}

func optionalBool(c nbt.Compound, key string) string {
	if v, ok := c[key]; ok {
		return textNumberMalformed(v)
	}
	return ""
}

// vanilla: ExtraCodecs.intRange, NbtOps.getNumberValue
func optionalRange(c nbt.Compound, key string, lo, hi int32) string {
	v, ok := c[key]
	if !ok {
		return ""
	}
	n, isNum := nbt.IntValue(v)
	if !isNum {
		return "Not a number"
	}
	if n < lo || n > hi {
		return "Value must be within range [" + strconv.Itoa(int(lo)) + ";" + strconv.Itoa(int(hi)) + "]: " + strconv.Itoa(int(n))
	}
	return ""
}

// boolOf reads any nonzero number as true.
func boolOf(v nbt.Tag) (bool, bool) {
	_, ok := nbt.ByteValue(v)
	return protocol.FlagOn(v), ok
}

// dialogFloatOf narrows numbers to the float32 used by Codec.FLOAT.
func dialogFloatOf(v nbt.Tag) (float64, bool) {
	switch n := v.(type) {
	case nbt.Float:
		return float64(n), true
	case nbt.Double:
		return float64(float32(n)), true
	case nbt.Long:
		return float64(float32(n)), true
	}
	if n, ok := nbt.IntValue(v); ok {
		return float64(float32(n)), true
	}
	return 0, false
}

// vanilla: ActionTypes.bootstrap
var actionTypes = map[string]struct{}{
	"open_url": {}, "run_command": {}, "suggest_command": {}, "show_dialog": {},
	"change_page": {}, "copy_to_clipboard": {}, "custom": {},
	"dynamic/run_command": {}, "dynamic/custom": {},
}

// vanilla: TextInput, NumberRangeInput, SingleOptionInput, BooleanInput
func inputControlCheck(in nbt.Compound) string {
	if msg := textFieldMalformed(in, "type", func(tag nbt.Tag) string {
		return textRegistryMalformed(tag, "minecraft:input_control_type", func(name string) bool {
			switch name {
			case "minecraft:boolean", "minecraft:text", "minecraft:single_option", "minecraft:number_range":
				return true
			}
			return false
		})
	}); msg != "" {
		return msg
	}
	if msg := requiredComponent(in, "label"); msg != "" {
		return msg
	}
	switch DialogKind(in) {
	case "boolean":
		if msg := optionalBool(in, "initial"); msg != "" {
			return msg
		}
		return optionalStrings(in, "on_true", "on_false")
	case "text":
		return textInputCheck(in)
	case "single_option":
		return optionCheck(in)
	case "number_range":
		return numberRangeCheck(in)
	}
	return "Unknown input control type " + nbt.Stringify(in["type"])
}

func optionalStrings(c nbt.Compound, keys ...string) string {
	for _, key := range keys {
		if v, ok := c[key]; ok {
			if _, isString := v.(nbt.String); !isString {
				return "Not a string: " + nbt.Stringify(v)
			}
		}
	}
	return ""
}

func textInputCheck(in nbt.Compound) string {
	if msg := optionalRange(in, "width", 1, 1024); msg != "" {
		return msg
	}
	if msg := optionalBool(in, "label_visible"); msg != "" {
		return msg
	}
	if msg := optionalStrings(in, "initial"); msg != "" {
		return msg
	}
	maxLength := int64(32)
	if v, ok := in["max_length"]; ok {
		n, isNum := nbt.IntValue(v)
		if !isNum || n < 1 {
			return "Value must be positive: " + nbt.Stringify(v)
		}
		maxLength = int64(n)
	}
	// Java's String.length counts UTF-16 units.
	if s, ok := in["initial"].(nbt.String); ok && int64(len(utf16.Encode([]rune(string(s))))) > maxLength {
		return "Default text length exceeds allowed size"
	}
	if v, ok := in["multiline"]; ok {
		m, isCompound := nbt.CompoundView(v)
		if !isCompound {
			return "Not a map: " + nbt.Stringify(v)
		}
		if n, has := m["max_lines"]; has {
			if k, isNum := nbt.IntValue(n); !isNum || k < 1 {
				return "Value must be positive: " + nbt.Stringify(n)
			}
		}
		return optionalRange(m, "height", 1, 512)
	}
	return ""
}

// optionCheck checks a single_option control: a non-empty "options" list of
// ids or {id, display, initial} entries, at most one of them initial.
func optionCheck(in nbt.Compound) string {
	if msg := optionalRange(in, "width", 1, 1024); msg != "" {
		return msg
	}
	if msg := optionalBool(in, "label_visible"); msg != "" {
		return msg
	}
	v, ok := in["options"]
	if !ok {
		return "No key options in " + nbt.Stringify(in)
	}
	l, isList := v.(nbt.List)
	if !isList {
		return "Not a list: " + nbt.Stringify(v)
	}
	if len(l.Items) == 0 {
		return "List must have contents"
	}
	initials := 0
	for _, item := range l.Items {
		switch o := item.(type) {
		case nbt.String:
		case nbt.CompoundTag:
			if _, isString := o.Entries()["id"].(nbt.String); !isString {
				return "No key id in " + nbt.Stringify(o)
			}
			if msg := optionalComponent(o.Entries(), "display"); msg != "" {
				return msg
			}
			if msg := optionalBool(o.Entries(), "initial"); msg != "" {
				return msg
			}
			if b, _ := boolOf(o.Entries()["initial"]); b {
				initials++
			}
		default:
			return "Not a map: " + nbt.Stringify(item)
		}
	}
	if initials > 1 {
		return "Multiple initial values"
	}
	return ""
}

// numberRangeCheck applies float32 bounds and range restrictions.
func numberRangeCheck(in nbt.Compound) string {
	if msg := optionalRange(in, "width", 1, 1024); msg != "" {
		return msg
	}
	if msg := optionalStrings(in, "label_format"); msg != "" {
		return msg
	}
	var bounds [2]float64
	for i, key := range []string{"start", "end"} {
		v, ok := in[key]
		if !ok {
			return "No key " + key + " in MapLike[" + nbt.Stringify(in) + "]"
		}
		f, isNum := dialogFloatOf(v)
		if !isNum {
			return "Not a number: " + nbt.Stringify(v)
		}
		bounds[i] = float64(float32(f)) // vanilla stores the bounds as floats
	}
	if v, ok := in["step"]; ok {
		if f, isNum := dialogFloatOf(v); !isNum || f <= 0 || f > math.MaxFloat32 {
			if f > math.MaxFloat32 {
				// vanilla prints the float it decoded, not the tag.
				return "Value must be positive: Infinity"
			}
			return "Value must be positive: " + nbt.Stringify(v)
		}
	}
	if v, ok := in["initial"]; ok {
		f, isNum := dialogFloatOf(v)
		if !isNum {
			return "Not a number: " + nbt.Stringify(v)
		}
		f = float64(float32(f))
		lo, hi := min(bounds[0], bounds[1]), max(bounds[0], bounds[1])
		if f < lo || f > hi {
			return "Initial value " + nbt.Stringify(v) + " is outside of range [" + strconv.FormatFloat(lo, 'g', -1, 32) + ", " + strconv.FormatFloat(hi, 'g', -1, 32) + "]"
		}
	}
	return ""
}

// DialogRegistry is the dialog holder registry.
const DialogRegistry = "minecraft:dialog"

// vanilla: DialogTypes.bootstrap
var dialogTypes = map[string]struct{}{
	"notice": {}, "server_links": {}, "dialog_list": {}, "multi_action": {}, "confirmation": {},
}
