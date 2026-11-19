package protocol

import (
	"slices"
)

// maxLoreLines bounds a lore list.
// vanilla: ItemLore.STREAM_CODEC
const maxLoreLines = 256

// registrySizeAttribute is the minecraft:attribute registry's size, from the
// vanilla data report.
const registrySizeAttribute = 40

// AttributeModifierEntry is one entry of minecraft:attribute_modifiers.
// vanilla: ItemAttributeModifiers.Entry
type AttributeModifierEntry struct {
	Attribute int32 // minecraft:attribute registry ID
	ID        string
	Amount    float64
	Operation int32 // AttributeModifier.Operation id
	Slot      int32 // EquipmentSlotGroup id
	Display   int32 // 0 default, 1 hidden; override text is not held
}

// SetLore lays minecraft:lore on the stack.
func (s *ItemStack) SetLore(lines []Text) {
	var value Buffer
	value.PutVarInt(int32(len(lines)))
	for _, l := range lines {
		value.PutText(l)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentLore, Data: value.Bytes()})
}

// LoreOf returns the stack's minecraft:lore lines; nil when it carries none.
func (s ItemStack) LoreOf() []Text {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentLore {
			continue
		}
		r := NewReader(raw.Data)
		n := r.ReadVarInt()
		if r.Err() != nil || n < 0 || n > maxLoreLines {
			return nil
		}
		lines := make([]Text, 0, n)
		for range n {
			t, ok := ReadText(r)
			if !ok {
				return nil
			}
			lines = append(lines, t)
		}
		return lines
	}
	return nil
}

func readLore(r *Reader) bool {
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 || n > maxLoreLines {
		return false
	}
	for range n {
		if _, ok := ReadText(r); !ok {
			return false
		}
	}
	return true
}

// SetAttributeModifiers lays minecraft:attribute_modifiers on the stack.
func (s *ItemStack) SetAttributeModifiers(entries []AttributeModifierEntry) {
	var value Buffer
	value.PutVarInt(int32(len(entries)))
	for _, e := range entries {
		value.PutVarInt(e.Attribute)
		value.PutString(e.ID)
		value.PutF64(e.Amount)
		value.PutVarInt(e.Operation)
		value.PutVarInt(e.Slot)
		value.PutVarInt(e.Display)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentAttributeModifiers, Data: value.Bytes()})
}

// AttributeModifiersOf returns the stack's minecraft:attribute_modifiers
// entries, and false when it carries none or ones with override text.
func (s ItemStack) AttributeModifiersOf() ([]AttributeModifierEntry, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentAttributeModifiers {
			continue
		}
		return readAttributeModifierEntries(NewReader(raw.Data), false)
	}
	return nil, false
}

// AttributeModifiersEffective is AttributeModifiersOf with entries that
// carry override text kept: what the stack applies when worn, not what it
// can round-trip.
func (s ItemStack) AttributeModifiersEffective() ([]AttributeModifierEntry, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentAttributeModifiers {
			return readAttributeModifierEntries(NewReader(raw.Data), true)
		}
	}
	return nil, false
}

// readAttributeModifierEntries reads the entries; override text (display 2)
// is skipped when skipText and otherwise makes the read fail, since an entry
// cannot hold it.
func readAttributeModifierEntries(r *Reader, skipText bool) ([]AttributeModifierEntry, bool) {
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 || n > 4096 {
		return nil, false
	}
	entries := make([]AttributeModifierEntry, 0, n)
	for range n {
		e := AttributeModifierEntry{Attribute: r.ReadVarInt(), ID: r.ReadString(32767),
			Amount: r.ReadF64(), Operation: r.ReadVarInt(), Slot: r.ReadVarInt(), Display: r.ReadVarInt()}
		if r.Err() != nil || e.Attribute < 0 || e.Attribute >= registrySizeAttribute ||
			e.Operation < 0 || e.Operation > 2 || e.Slot < 0 || e.Slot > 10 {
			return nil, false
		}
		switch e.Display {
		case 0, 1:
		case 2:
			if !skipText {
				return nil, false
			}
			if _, ok := ReadText(r); !ok {
				return nil, false
			}
		default:
			return nil, false
		}
		entries = append(entries, e)
	}
	return entries, true
}

func readAttributeModifiers(r *Reader) bool {
	_, ok := readAttributeModifierEntries(r, true)
	return ok
}

// HiddenTooltipOf returns the stack's minecraft:tooltip_display: the hide
// flag and the component type IDs it hides, in order.
func (s ItemStack) HiddenTooltipOf() (hide bool, components []int32) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentTooltipDisplay {
			continue
		}
		r := NewReader(raw.Data)
		hide = r.ReadBool()
		n := r.ReadVarInt()
		if r.Err() != nil || n < 0 || n > maxHiddenComponents {
			return false, nil
		}
		for range n {
			components = append(components, r.ReadVarInt())
		}
		return hide, components
	}
	return false, nil
}

// SetTooltipDisplay lays minecraft:tooltip_display on the stack.
func (s *ItemStack) SetTooltipDisplay(hide bool, components []int32) {
	var value Buffer
	value.PutBool(hide)
	value.PutVarInt(int32(len(components)))
	for _, id := range components {
		value.PutVarInt(id)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentTooltipDisplay, Data: value.Bytes()})
}
