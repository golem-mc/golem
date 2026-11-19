package protocol

import "slices"

// maxHiddenComponents bounds a tooltip_display list; one entry per data
// component type is the most a codec that rejects duplicates can carry.
const maxHiddenComponents = 256

// SetItemName lays minecraft:item_name on the stack, the name an item shows
// when it has no custom name.
func (s *ItemStack) SetItemName(name Text) {
	var value Buffer
	value.PutText(name)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentItemName, Data: value.Bytes()})
}

// SetRarity lays minecraft:rarity on the stack, a Rarity ordinal.
func (s *ItemStack) SetRarity(rarity int32) {
	var value Buffer
	value.PutVarInt(rarity)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentRarity, Data: value.Bytes()})
}

// RarityOf returns the stack's minecraft:rarity ordinal, if present.
func (s ItemStack) RarityOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentRarity {
			r := NewReader(raw.Data)
			rarity := r.ReadVarInt()
			return rarity, r.Err() == nil && r.Remaining() == 0
		}
	}
	return 0, false
}

// SetHiddenTooltipComponents lays minecraft:tooltip_display on the stack,
// hiding the named data components from the tooltip.
func (s *ItemStack) SetHiddenTooltipComponents(components ...int32) {
	var value Buffer
	value.PutBool(false)
	value.PutVarInt(int32(len(components)))
	for _, id := range components {
		value.PutVarInt(id)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentTooltipDisplay, Data: value.Bytes()})
}

// readTooltipDisplay reads a hide flag and a list of component type IDs.
// vanilla: TooltipDisplay.STREAM_CODEC
func readTooltipDisplay(r *Reader) bool {
	r.ReadBool()
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 || n > maxHiddenComponents {
		return false
	}
	for range n {
		r.ReadVarInt()
	}
	return r.Err() == nil
}

// ItemNameOf returns the item name component, if present.
func (s ItemStack) ItemNameOf() (Text, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentItemName {
			r := NewReader(raw.Data)
			text, ok := ReadText(r)
			return text, ok && r.Remaining() == 0
		}
	}
	return Text{}, false
}

// SetItemModel lays minecraft:item_model on the stack.
func (s *ItemStack) SetItemModel(model string) { s.setIdentifier(DataComponentItemModel, model) }

// ItemModelOf returns the stack's minecraft:item_model, if present.
func (s ItemStack) ItemModelOf() (string, bool) { return s.identifierOf(DataComponentItemModel) }

// SetTooltipStyle lays minecraft:tooltip_style on the stack.
func (s *ItemStack) SetTooltipStyle(style string) { s.setIdentifier(DataComponentTooltipStyle, style) }

// TooltipStyleOf returns the stack's minecraft:tooltip_style, if present.
func (s ItemStack) TooltipStyleOf() (string, bool) { return s.identifierOf(DataComponentTooltipStyle) }

func (s *ItemStack) setIdentifier(component int32, id string) {
	var value Buffer
	value.PutString(id)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: component, Data: value.Bytes()})
}

func (s ItemStack) identifierOf(component int32) (string, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			r := NewReader(raw.Data)
			id := r.ReadString(32767)
			return id, r.Err() == nil && r.Remaining() == 0
		}
	}
	return "", false
}

// BreakSoundOf returns the stack's minecraft:break_sound, if present.
func (s ItemStack) BreakSoundOf() (SoundHolder, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBreakSound {
			continue
		}
		r := NewReader(raw.Data)
		h, ok := readSoundHolder(r)
		return h, ok && r.Remaining() == 0
	}
	return SoundHolder{}, false
}
