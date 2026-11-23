package registry

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

const componentCodecRefusal = "No matching codec found"

// ComponentValueMalformed validates modelled components; unmodelled codecs remain unchecked.
// vanilla: DataComponentType.codecOrThrow
func ComponentValueMalformed(key string, tag nbt.Tag) string {
	switch key {
	case "minecraft:damage", "minecraft:repair_cost":
		if s, isString := tag.(nbt.String); isString {
			// A string whose name escape the reader has no table for is a
			// value only the shape of; nothing more can be checked of it.
			if strings.ContainsRune(string(s), '\uFFFD') {
				return ""
			}
			return "Not a number"
		}
		n, ok := nbt.IntValue(tag)
		if !ok {
			return "Not a number"
		}
		if n < 0 {
			return "Value must be non-negative: " + strconv.FormatInt(int64(n), 10)
		}
	case "minecraft:max_stack_size":
		// vanilla: DataComponents.MAX_STACK_SIZE, ExtraCodecs.intRange
		n, ok := nbt.IntValue(tag)
		if !ok {
			return "Not a number"
		}
		if n < protocol.MinMaxStackSize || n > protocol.MaxMaxStackSize {
			return fmt.Sprintf("Value must be within range [%d;%d]: %d", protocol.MinMaxStackSize, protocol.MaxMaxStackSize, n)
		}
	case "minecraft:dyed_color":
		// vanilla: ExtraCodecs.RGB_COLOR_CODEC
		if ComponentRefused(key, tag) {
			switch tag.(type) {
			case nbt.List, nbt.ByteArray, nbt.IntArray, nbt.LongArray:
				return componentCodecRefusal
			default:
				return "Failed to parse either. First: Not a number; Second: Not a list: " + nbt.Stringify(tag)
			}
		}
	case "minecraft:entity_data", "minecraft:block_entity_data":
		// vanilla: TypedEntityData.codec
		if ComponentRefused(key, tag) {
			if data, ok := compoundTagOrSNBT(tag); ok {
				if _, hasID := data.Entries()["id"]; !hasID {
					return "Expected 'id' field in " + nbt.Stringify(tag)
				}
			}
			return componentCodecRefusal
		}
	case "minecraft:unbreakable":
		if _, ok := nbt.CompoundView(tag); !ok {
			return "Not a map: " + nbt.Stringify(tag)
		}
	case "minecraft:enchantment_glint_override":
		if _, ok := nbt.IntValue(tag); !ok {
			return "Not a number"
		}
	case "minecraft:count":
		return MinMaxIntsTagMalformed(tag)
	case "minecraft:custom_name", "minecraft:item_name":
		return TextComponentMalformed(tag)
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		return enchantmentsComponentTagMalformed(tag)
	case "minecraft:block_state":
		return blockStateComponentTagMalformed(tag)
	case "minecraft:banner_patterns":
		return bannerPatternsTagMalformed(tag)
	case "minecraft:profile":
		return ProfileComponentMalformed(tag)
	case "minecraft:use_remainder":
		// vanilla: UseRemainder.CODEC
		if msg, _ := itemTemplateDiagnostic(tag); msg != "" {
			return msg
		}
		if ComponentRefused(key, tag) {
			return componentCodecRefusal
		}
	case "minecraft:note_block_sound":
		return resourceLocationMalformed(tag)
	case "minecraft:writable_book_content", "minecraft:written_book_content":
		// vanilla: WrittenBookContent.CODEC, WritableBookContent.CODEC
		var probe protocol.ItemStack
		if !ReadComponent(&probe, key, tag) {
			return "Invalid book content"
		}

	case "minecraft:potion_contents":
		// The alternative codec reports both of its failures.
		// vanilla: PotionContents.CODEC
		switch tag.(type) {
		case nbt.CompoundTag, nbt.String:
		default:
			text := nbt.Stringify(tag)
			return "Failed to parse either. First: Not a map: " + text + "; Second: Not a string: " + text
		}
		if ComponentRefused(key, tag) {
			return componentCodecRefusal
		}
	default:
		if _, fields := recordFieldsOf(key); fields != nil {
			if msg := recordMalformed(fields, tag); msg != "" {
				return msg
			}
		}
		if ComponentRefused(key, tag) {
			return componentCodecRefusal
		}
	}
	return ""
}

// resourceLocationMalformed preserves the identifier codec's error wording.
// vanilla: Identifier.CODEC, Identifier.parse
func resourceLocationMalformed(tag nbt.Tag) string {
	s, isString := tag.(nbt.String)
	if !isString {
		return "Not a string"
	}
	return protocol.IdentifierError(string(s))
}

// blockStateComponentTagMalformed checks string values; placement checks property names.
// vanilla: BlockItemStateProperties.CODEC
func blockStateComponentTagMalformed(tag nbt.Tag) string {
	entries, isMap := nbt.CompoundView(tag)
	if !isMap {
		return "Not a map: " + nbt.Stringify(tag)
	}
	for _, value := range entries {
		if _, ok := value.(nbt.String); !ok {
			return "Not a string missed input: " + nbt.Stringify(tag)
		}
	}
	return ""
}

// enchantmentsComponentTagMalformed checks keys before levels, preserving codec error order.
// vanilla: ItemEnchantments.CODEC
func enchantmentsComponentTagMalformed(tag nbt.Tag) string {
	entries, isMap := nbt.CompoundView(tag)
	if !isMap {
		return "Not a map: " + nbt.Stringify(tag)
	}
	for name, level := range entries {
		qualified := Qualify(name)
		if _, known := EnchantmentID(qualified); !known {
			return "Failed to get element " + qualified + " missed input: " + nbt.Stringify(tag)
		}
		n, ok := nbt.IntValue(level)
		if !ok {
			return "Not a number missed input: " + nbt.Stringify(tag)
		}
		if n < 1 || n > 255 {
			return "Value " + strconv.FormatInt(int64(n), 10) + " outside of range [1:255] missed input: " + nbt.Stringify(tag)
		}
	}
	return ""
}

// vanilla: BannerPatternLayers.CODEC
func bannerPatternsTagMalformed(tag nbt.Tag) string {
	_, refusal := ParseBannerPatterns(tag)
	return refusal
}

// ParseBannerPatterns reads layers, reporting the last codec refusal first.
// vanilla: BannerPatternLayers.CODEC, Codec.listOf
func ParseBannerPatterns(tag nbt.Tag) ([]protocol.BannerPatternLayer, string) {
	list, isList := tag.(nbt.List)
	if !isList {
		return nil, "Not a list: " + nbt.Stringify(tag)
	}
	patterns, _ := Get("minecraft:banner_pattern")
	layers := make([]protocol.BannerPatternLayer, 0, len(list.Items))
	refusal := ""
	for _, item := range list.Items {
		layer, msg := bannerPatternLayerTagMalformed(nbt.ListElement(item), patterns)
		if msg != "" {
			refusal = JoinFieldErrors(msg, refusal)
			continue
		}
		layers = append(layers, layer)
	}
	if refusal != "" {
		return nil, refusal
	}
	return layers, ""
}

// bannerPatternLayerTagMalformed reports colour failures before pattern failures.
// vanilla: BannerPatternLayers.Layer.CODEC
func bannerPatternLayerTagMalformed(tag nbt.Tag, patterns Registry) (protocol.BannerPatternLayer, string) {
	fields, isMap := nbt.CompoundView(tag)
	if !isMap {
		return protocol.BannerPatternLayer{}, "Not a map: " + nbt.Stringify(tag)
	}
	colorRefusal, dye := bannerDyeTagMalformed(fields)
	patternRefusal, layer := bannerPatternTagMalformed(fields, patterns)
	layer.Dye = dye
	return layer, JoinFieldErrors(colorRefusal, patternRefusal)
}

// bannerDyeTagMalformed returns -1 when the dye name does not decode.
// vanilla: DyeColor.CODEC
func bannerDyeTagMalformed(fields nbt.Compound) (string, int32) {
	tag, ok := fields["color"]
	if !ok {
		return "No key color in MapLike[" + nbt.Stringify(fields) + "]", -1
	}
	name, isString := tag.(nbt.String)
	if !isString {
		return "Not a string", -1
	}
	for id, dye := range DyeNames {
		if dye == string(name) {
			return "", int32(id)
		}
	}
	return "Unknown element name:" + string(name), -1
}

// bannerPatternTagMalformed reads a registry holder or an inline pattern.
// vanilla: BannerPattern.CODEC, RegistryFileCodec
func bannerPatternTagMalformed(fields nbt.Compound, patterns Registry) (string, protocol.BannerPatternLayer) {
	layer := protocol.BannerPatternLayer{Pattern: -1}
	tag, ok := fields["pattern"]
	if !ok {
		return "No key pattern in MapLike[" + nbt.Stringify(fields) + "]", layer
	}
	if id, isString := tag.(nbt.String); isString && protocol.ValidIdentifier(string(id)) {
		name := Qualify(string(id))
		if pos := patterns.ID(name); pos >= 0 {
			layer.Pattern = pos
			return "", layer
		}
		return "Failed to get element ResourceKey[" + patterns.Name + " / " + name + "]", layer
	}
	return bannerInlinePatternTagMalformed(tag, layer)
}

// bannerInlinePatternTagMalformed folds translation-key errors before asset errors.
// vanilla: BannerPattern.DIRECT_CODEC, Identifier.CODEC
func bannerInlinePatternTagMalformed(tag nbt.Tag, layer protocol.BannerPatternLayer) (string, protocol.BannerPatternLayer) {
	fields, isMap := nbt.CompoundView(tag)
	if !isMap {
		return "Not a map: " + nbt.Stringify(tag), layer
	}
	keyRefusal, translationKey := bannerTranslationKeyTagMalformed(fields)
	assetRefusal, asset := bannerAssetTagMalformed(fields)
	if msg := JoinFieldErrors(keyRefusal, assetRefusal); msg != "" {
		return msg, layer
	}
	layer.AssetID = asset
	layer.TranslationKey = translationKey
	return "", layer
}

// vanilla: BannerPattern.assetId, Identifier.CODEC
func bannerAssetTagMalformed(fields nbt.Compound) (string, string) {
	tag, ok := fields["asset_id"]
	if !ok {
		return "No key asset_id in MapLike[" + nbt.Stringify(fields) + "]", ""
	}
	if refusal := resourceLocationMalformed(tag); refusal != "" {
		return refusal, ""
	}
	return "", string(tag.(nbt.String))
}

// vanilla: BannerPattern.translationKey, Codec.STRING
func bannerTranslationKeyTagMalformed(fields nbt.Compound) (string, string) {
	tag, ok := fields["translation_key"]
	if !ok {
		return "No key translation_key in MapLike[" + nbt.Stringify(fields) + "]", ""
	}
	s, isString := tag.(nbt.String)
	if !isString {
		return "Not a string", ""
	}
	return "", string(s)
}

// TextComponentMalformed returns the component codec's diagnostic, or "" on success.
// vanilla: ComponentSerialization.CODEC
func TextComponentMalformed(tag nbt.Tag) string {
	msg, _ := textComponentDiagnostic(tag)
	return msg
}

// ParseTextStyle reads the style fields, preserving fields outside the Text model.
// vanilla: Style.Serializer.CODEC
func ParseTextStyle(tag nbt.Tag) (nbt.Compound, string) {
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, "Not a map: " + nbt.Stringify(tag)
	}
	if msg := textStyleMalformed(fields); msg != "" {
		return nil, msg
	}
	style := nbt.Compound{}
	for _, key := range textStyleFields {
		if value, present := fields[key]; present {
			style[key] = nbt.Snapshot(value)
		}
	}
	return style, ""
}

// textComponentDiagnostic retains whether decoding produced a partial value:
// EitherCodec suppresses its alternative's error when a partial value exists.
func textComponentDiagnostic(tag nbt.Tag) (string, bool) {
	switch v := tag.(type) {
	case nbt.String:
		return "", true
	case nbt.List, nbt.ByteArray, nbt.IntArray, nbt.LongArray:
		msg, partial := textComponentListDiagnostic(tag)
		if partial {
			return msg, true
		}
		return textComponentEither(msg, "Not a map: "+nbt.Stringify(tag)), false
	case nbt.CompoundTag:
		fields := v.Entries()
		content, partial := textContentDiagnostic(fields)
		extra := ""
		if value, present := fields["extra"]; present {
			extra, _ = textComponentListDiagnostic(value)
		}
		msg := JoinFieldErrors(textStyleMalformed(fields), extra, content)
		if partial {
			return msg, true
		}
		return textComponentEither("Not a list: "+nbt.Stringify(tag), msg), false
	default:
		return textComponentEither("Not a list: "+nbt.Stringify(tag), "Not a map: "+nbt.Stringify(tag)), false
	}
}

// vanilla: ExtraCodecs.nonEmptyList, ListCodec.decode
func textComponentListDiagnostic(tag nbt.Tag) (string, bool) {
	return codecListDiagnostic(tag, textComponentDiagnostic, true)
}

// codecListDiagnostic retains the partial list even when all elements fail.
// A non-empty-list validation loses that partial when no element survived.
// vanilla: ListCodec.decode, ExtraCodecs.nonEmptyList
func codecListDiagnostic(tag nbt.Tag, read func(nbt.Tag) (string, bool), nonempty bool) (string, bool) {
	items, ok := textListItems(tag)
	if !ok {
		return "Not a list: " + nbt.Stringify(tag), false
	}
	msg, count := "", 0
	for _, item := range items {
		child, partial := read(nbt.ListElement(item))
		msg = JoinFieldErrors(child, msg)
		if partial {
			count++
		}
	}
	if nonempty && count == 0 {
		msg = JoinFieldErrors(msg, "List must have contents")
	}
	return msg, !nonempty || count > 0
}

// vanilla: EitherCodec.decode
func codecAlternative(tag nbt.Tag, first, second func(nbt.Tag) (string, bool)) (string, bool) {
	msg, partial := first(tag)
	if msg == "" || partial {
		return msg, partial
	}
	other, partial := second(tag)
	if other == "" || partial {
		return other, partial
	}
	return profileEitherMalformed(msg, other), false
}

// vanilla: ExtraCodecs.compactListCodec
func compactListDiagnostic(tag nbt.Tag, read func(nbt.Tag) (string, bool)) (string, bool) {
	return codecAlternative(tag, func(tag nbt.Tag) (string, bool) {
		return codecListDiagnostic(tag, read, false)
	}, read)
}

func codecFieldDiagnostic(fields nbt.Compound, key string, read func(nbt.Tag) (string, bool)) (string, bool) {
	value, present := fields[key]
	if !present {
		return "No key " + key + " in MapLike[" + nbt.Stringify(fields) + "]", false
	}
	return read(value)
}

// vanilla: Style.Serializer.MAP_CODEC
var textStyleFields = []string{
	"color", "shadow_color", "bold", "italic", "underlined", "strikethrough",
	"obfuscated", "click_event", "hover_event", "insertion", "font",
}

func textStyleMalformed(fields nbt.Compound) string {
	msg := ""
	for _, name := range textStyleFields {
		value, present := fields[name]
		if !present {
			continue
		}
		field := ""
		switch name {
		case "color":
			if s, ok := value.(nbt.String); ok {
				field = textColorMessage(string(s))
			} else {
				field = "Not a string"
			}
		case "shadow_color":
			field = shadowColorMalformed(value)
		case "bold", "italic", "underlined", "strikethrough", "obfuscated":
			field = textNumberMalformed(value)
		case "insertion":
			field = textStringMalformed(value)
		case "font":
			field = resourceLocationMalformed(value)
		case "click_event", "hover_event":
			event, ok := nbt.CompoundView(value)
			if !ok {
				field = "Not a map: " + nbt.Stringify(value)
			} else if name == "click_event" {
				field = clickEventMalformed(event)
			} else {
				field = hoverEventMalformed(event)
			}
		}
		msg = JoinFieldErrors(field, msg)
	}
	return msg
}

func textStringMalformed(tag nbt.Tag) string {
	if _, ok := tag.(nbt.String); !ok {
		return "Not a string"
	}
	return ""
}

func textNumberMalformed(tag nbt.Tag) string {
	if _, ok := nbt.NumberValue(tag); !ok {
		return "Not a number"
	}
	return ""
}

// textFieldMalformed distinguishes missing required fields from bad values.
func textFieldMalformed(fields nbt.Compound, name string, check func(nbt.Tag) string) string {
	value, present := fields[name]
	if !present {
		return "No key " + name + " in MapLike[" + nbt.Stringify(fields) + "]"
	}
	return check(value)
}

// vanilla: TextColor.parseColor
func validTextColor(s string) bool { return textColorMessage(s) == "" }

// textColorMessage words why a colour string does not parse, or "" where it
// does. vanilla: TextColor.parseColor
func textColorMessage(s string) string {
	if strings.HasPrefix(s, "#") {
		value, err := strconv.ParseInt(s[1:], 16, 32)
		switch {
		case err != nil:
			return "Invalid color value: " + s
		case value < 0 || value > 0xFFFFFF:
			return "Color value out of range: " + s
		}
		return ""
	}
	if !textNamedColors[s] {
		return "Invalid color name: " + s
	}
	return ""
}

// textNamedColors is the sixteen colours TextColor names.
var textNamedColors = map[string]bool{
	"black": true, "dark_blue": true, "dark_green": true, "dark_aqua": true,
	"dark_red": true, "dark_purple": true, "gold": true, "gray": true,
	"dark_gray": true, "blue": true, "green": true, "aqua": true,
	"red": true, "light_purple": true, "yellow": true, "white": true,
}

// vanilla: ExtraCodecs.ARGB_COLOR_CODEC
func shadowColorMalformed(tag nbt.Tag) string {
	if textNumberMalformed(tag) == "" {
		return ""
	}
	listError, count := "", 0
	items, ok := textListItems(tag)
	if !ok {
		return profileEitherMalformed("Not a number", "Not a list: "+nbt.Stringify(tag))
	}
	for _, item := range items {
		if msg := textNumberMalformed(item); msg != "" {
			listError = JoinFieldErrors(msg, listError)
		} else {
			count++
		}
	}
	if count != 4 {
		listError = JoinFieldErrors(listError, "Input is not a list of 4 elements")
	}
	if listError == "" {
		return ""
	}
	return profileEitherMalformed("Not a number", listError)
}

// vanilla: NbtOps.getStream
func textListItems(tag nbt.Tag) ([]nbt.Tag, bool) {
	switch v := tag.(type) {
	case nbt.List:
		return v.Items, true
	case nbt.ByteArray:
		items := make([]nbt.Tag, len(v))
		for i, n := range v {
			items[i] = nbt.Byte(n)
		}
		return items, true
	case nbt.IntArray:
		items := make([]nbt.Tag, len(v))
		for i, n := range v {
			items[i] = nbt.Int(n)
		}
		return items, true
	case nbt.LongArray:
		items := make([]nbt.Tag, len(v))
		for i, n := range v {
			items[i] = nbt.Long(n)
		}
		return items, true
	}
	return nil, false
}

// vanilla: ClickEvent.CODEC, ClickEvent.Action.isAllowedFromServer
func clickEventMalformed(fields nbt.Compound) string {
	if msg := textFieldMalformed(fields, "action", textStringMalformed); msg != "" {
		return msg
	}
	return clickActionMalformed(string(fields["action"].(nbt.String)), fields)
}

// vanilla: ClickEvent payload codecs, also wrapped by StaticAction.
func clickActionMalformed(action string, fields nbt.Compound) string {
	key, check := "", textStringMalformed
	switch action {
	case "open_file":
		return "Click event type not allowed: OPEN_FILE"
	case "open_url":
		key, check = "url", textURIMalformed
	case "run_command", "suggest_command":
		key, check = "command", textChatStringMalformed
	case "show_dialog":
		key, check = "dialog", DialogRefMalformed
	case "change_page":
		key, check = "page", func(tag nbt.Tag) string {
			n, ok := nbt.IntValue(tag)
			if !ok {
				return "Not a number"
			}
			if n <= 0 {
				return "Value must be positive: " + strconv.FormatInt(int64(n), 10)
			}
			return ""
		}
	case "copy_to_clipboard":
		key = "value"
	case "custom":
		key, check = "id", resourceLocationMalformed
	default:
		return "Unknown element name:" + action
	}
	return textFieldMalformed(fields, key, check)
}

// vanilla: ExtraCodecs.CHAT_STRING
func textChatStringMalformed(tag nbt.Tag) string {
	if msg := textStringMalformed(tag); msg != "" {
		return msg
	}
	for _, c := range string(tag.(nbt.String)) {
		if c == '§' || c < ' ' || c == 127 {
			return "Disallowed chat character: '" + string(c) + "'"
		}
	}
	return ""
}

// vanilla: HoverEvent.CODEC
func hoverEventMalformed(fields nbt.Compound) string {
	if msg := textFieldMalformed(fields, "action", textStringMalformed); msg != "" {
		return msg
	}
	switch action := string(fields["action"].(nbt.String)); action {
	case "show_text":
		return textFieldMalformed(fields, "value", TextComponentMalformed)
	case "show_entity":
		msg := ""
		if name, present := fields["name"]; present {
			msg = TextComponentMalformed(name)
		}
		return JoinFieldErrors(msg,
			textFieldMalformed(fields, "uuid", textUUIDMalformed),
			textFieldMalformed(fields, "id", func(tag nbt.Tag) string {
				return textRegistryMalformed(tag, "minecraft:entity_type", func(n string) bool { _, ok := EntityTypeID(n); return ok })
			}),
		)
	case "show_item":
		msg, _ := itemTemplateMapDiagnostic(fields)
		return msg
	default:
		return "Unknown element name:" + action
	}
}

// vanilla: ItemStackTemplate.MAP_CODEC
func itemTemplateMapDiagnostic(fields nbt.Compound) (string, bool) {
	required := textFieldMalformed(fields, "id", textItemMalformed)
	msg := ""
	if patch, present := fields["components"]; present {
		msg = textPatchMalformed(patch)
	}
	return JoinFieldErrors(msg, optionalRange(fields, "count", 1, 99), required), required == ""
}

// vanilla: ItemStackTemplate.CODEC
func itemTemplateDiagnostic(tag nbt.Tag) (string, bool) {
	return codecAlternative(tag, func(tag nbt.Tag) (string, bool) {
		fields, ok := nbt.CompoundView(tag)
		if !ok {
			return "Not a map: " + nbt.Stringify(tag), false
		}
		return itemTemplateMapDiagnostic(fields)
	}, func(tag nbt.Tag) (string, bool) { msg := textItemMalformed(tag); return msg, msg == "" })
}

// textRegistryMalformed validates a registry key against known; registry only names the diagnostic.
func textRegistryMalformed(tag nbt.Tag, registry string, known func(string) bool) string {
	if msg := resourceLocationMalformed(tag); msg != "" {
		return msg
	}
	name := Qualify(string(tag.(nbt.String)))
	if !known(name) {
		return "Unknown registry key in ResourceKey[minecraft:root / " + registry + "]: " + name
	}
	return ""
}

func textItemMalformed(tag nbt.Tag) string {
	msg := textRegistryMalformed(tag, "minecraft:item", func(n string) bool { _, ok := ItemID(n); return ok })
	if msg == "" && Qualify(string(tag.(nbt.String))) == "minecraft:air" {
		return "Item must not be minecraft:air"
	}
	return msg
}

func textUUIDMalformed(tag nbt.Tag) string {
	first := profileUUIDMalformed(tag)
	if first == "" {
		return ""
	}
	second := textStringMalformed(tag)
	if second == "" {
		s := string(tag.(nbt.String))
		if msg := protocol.UUIDStringMalformed(s); msg != "" {
			second = "Invalid UUID " + s + ": " + msg
		} else {
			return ""
		}
	}
	return profileEitherMalformed(first, second)
}

// vanilla: DataComponentPatch.CODEC, UnboundedMapCodec.decode
func textPatchMalformed(tag nbt.Tag) string {
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	msg := ""
	for _, key := range nbt.HashOrderedKeys(tag.(nbt.CompoundTag)) {
		value := fields[key]
		name := Qualify(strings.TrimPrefix(key, "!"))
		failure := ""
		switch {
		case !ItemComponents[name]:
			failure = "No component with type: '" + name + "'"
			failure = JoinFieldErrors(failure, failure)
		case TransientItemComponents[name]:
			failure = "'" + name + "' is not a persistent component"
			failure = JoinFieldErrors(failure, failure)
		case strings.HasPrefix(key, "!"):
			if _, ok := nbt.CompoundView(value); !ok {
				failure = "Not a map: " + nbt.Stringify(value)
			}
		default:
			failure = ComponentValueMalformed(name, value)
		}
		msg = JoinFieldErrors(failure, msg)
	}
	if msg != "" {
		msg += " missed input: " + nbt.Stringify(tag)
	}
	return msg
}

func textComponentEither(listErr, mapErr string) string {
	return "Failed to parse either. First: Failed to parse either. First: Not a string; Second: " +
		listErr + "; Second: " + mapErr
}

// MinMaxIntsTagMalformed reads an exact int or optional ordered bounds.
// vanilla: MinMaxBounds.Ints.CODEC
func MinMaxIntsTagMalformed(tag nbt.Tag) string {
	if c, isMap := nbt.CompoundView(tag); isMap {
		min, hadMin, minOK := MinMaxField(c, "min")
		max, hadMax, maxOK := MinMaxField(c, "max")
		if !minOK || !maxOK {
			return "Not a number"
		}
		if hadMin && hadMax && min > max {
			return fmt.Sprintf("Swapped bounds in range: Optional[%d] is higher than Optional[%d]", min, max)
		}
		return ""
	}
	if _, ok := nbt.IntValue(tag); ok {
		return ""
	}
	return "Failed to parse either. First: Not a map: " + nbt.Stringify(tag) + "; Second: Not a number"
}

// MinMaxField reads an optional int bound without treating absence as zero.
func MinMaxField(c nbt.Compound, name string) (bound int32, present, ok bool) {
	v, present := c[name]
	if !present {
		return 0, false, true
	}
	n, ok := nbt.IntValue(v)
	if !ok {
		return 0, true, false
	}
	return n, true, true
}

// ComponentDecodes reports whether a component fits the client decoder and its codec accepts it.
func ComponentDecodes(tag nbt.Tag) bool {
	return protocol.RawTextFits(tag) && TextComponentMalformed(tag) == ""
}
