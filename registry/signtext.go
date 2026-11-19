package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// SignTextTag writes a sign text component in its codec's NBT shape, the one
// a sign's front_text and back_text are saved in: messages, filtered_messages
// when set, color and has_glowing_text. A dye out of range is white.
// vanilla: SignText.CODEC, DyeColor.BY_ID
func SignTextTag(text protocol.SignText) *nbt.OrderedCompound {
	dye := text.Dye
	if dye < 0 || int(dye) >= len(DyeNames) {
		dye = 0
	}
	glow := nbt.Byte(0)
	if text.Glowing {
		glow = 1
	}
	tag := nbt.NewOrderedCompound(4)
	tag.Set("messages", signLinesTag(text.Messages))
	if text.Filtered != nil {
		tag.Set("filtered_messages", signLinesTag(*text.Filtered))
	}
	tag.Set("color", nbt.String(DyeNames[dye]))
	tag.Set("has_glowing_text", glow)
	return tag
}

// signLinesTag is four components as one NBT list.
func signLinesTag(lines [4]nbt.Tag) nbt.List {
	items := make([]nbt.Tag, 4)
	for i, line := range lines {
		if line == nil {
			line = nbt.String("")
		}
		items[i] = line
	}
	return nbt.NewList(items)
}

// SignTextOf reads a sign text component from its codec's NBT shape: exactly
// four messages, optional filtered_messages, a dye name (black when absent)
// and glow (false when absent); false for a shape the codec refuses.
// vanilla: SignText.CODEC
func SignTextOf(tag nbt.Compound) (protocol.SignText, bool) {
	var text protocol.SignText
	messages, ok := nbt.Get[nbt.List](tag, "messages")
	if !ok || !signLinesOf(messages, &text.Messages) {
		return protocol.SignText{}, false
	}
	// The filtered lines are read leniently: a bad list is dropped.
	// vanilla: SignText.CODEC lenientOptionalFieldOf
	if filtered, ok := nbt.Get[nbt.List](tag, "filtered_messages"); ok {
		var lines [4]nbt.Tag
		if signLinesOf(filtered, &lines) {
			text.Filtered = &lines
		}
	}
	text.Dye = int32(slices.Index(DyeNames, "black"))
	if value, ok := tag["color"]; ok {
		name, ok := value.(nbt.String)
		dye := slices.Index(DyeNames, string(name))
		if !ok || dye < 0 {
			return protocol.SignText{}, false
		}
		text.Dye = int32(dye)
	}
	if value, ok := tag["has_glowing_text"]; ok {
		// Codec.BOOL reads any number, true when not nought.
		// vanilla: NbtOps.getBooleanValue
		if _, ok := nbt.LongValue(value); !ok {
			return protocol.SignText{}, false
		}
		text.Glowing = nbt.DoubleFieldOr(tag, "has_glowing_text", 0) != 0
	}
	return text, true
}

// signLinesOf reads four text components, unwrapping a list element's ""
// wrapper.
func signLinesOf(list nbt.List, lines *[4]nbt.Tag) bool {
	if len(list.Items) != 4 {
		return false
	}
	for i, item := range list.Items {
		item = nbt.ListElement(item)
		if _, ok := protocol.TextOf(item); !ok && !protocol.IsTextComponent(item) {
			return false
		}
		lines[i] = item
	}
	return true
}
