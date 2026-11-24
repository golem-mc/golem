package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// parseSlotSource reads a slot source argument: a slot range word, a data
// pack source's id, or an inline source's SNBT. The returned name is the range
// word or qualified data-pack id, and empty for an inline source. The refusals
// stand at the argument's end.
// vanilla: SlotSourceArgument.parse, SlotSourceArgument.Result.name
func parseSlotSource(text string) (registry.SlotSource, string, *ParseError) {
	if _, known := itemSlotIDs[text]; known {
		return registry.SlotRangeSource{Source: "container", Slots: text}, text, nil
	}
	if name, n, ok := readIdentifierNonEmpty(text); ok {
		qualified := registry.Qualify(name)
		if source, found := registry.FindSlotSource(qualified); found {
			if n < len(text) {
				return nil, "", &ParseError{Key: keyExpectedSeparator, Cursor: n}
			}
			return source, qualified, nil
		}
		return nil, "", &ParseError{
			Key:    modifierNotFoundKey,
			With:   []string{qualified, "minecraft:slot_source"},
			Cursor: n,
		}
	}
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return nil, "", &ParseError{Key: slotUnknownKey, With: []string{text}, Cursor: len(text)}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		return nil, "", snbtRefusal(pe, joinSelector([]Token{{Text: text}}), pe.Offset)
	}
	if n != len(text) {
		return nil, "", &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	source, err := datapack.DecodeSlotSourceValue(nbtJSON(tag))
	if err != nil {
		return nil, "", &ParseError{Key: modifierFailedParseKey, With: []string{err.Error()}, Cursor: len(text)}
	}
	return source, "", nil
}

// checkSlotSourceArgument holds the argument to parseSlotSource while the
// walk reads it.
func checkSlotSourceArgument(text string) *ParseError {
	_, _, err := parseSlotSource(text)
	return err
}

// slotSourceCollection resolves a slot source on one holder of slots: the range
// sources' "container" is the holder and "this" the command's runner.
// vanilla: ItemCommands.getSlotsFromProvider
func slotSourceCollection(src Source, source registry.SlotSource, container func(name string) []rules.SlotAccess) rules.SlotCollection {
	ctx := rules.LootContext{ContainerSlots: container}
	if self := src.SelfEntity(); self != nil {
		ctx.This = lootEntityOf(self)
	}
	return rules.SlotSourceCollection(source, &ctx)
}

// slotSourceStacks is the stacks a source finds on one holder of slots.
// vanilla: ItemCommands.getSlotsFromProvider, SlotCollection.itemCopies
func slotSourceStacks(src Source, source registry.SlotSource, container func(name string) []rules.SlotAccess) []protocol.ItemStack {
	return slotSourceCollection(src, source, container).ItemCopies()
}

// entitySlotAccess returns read/write access to each slot a range word names
// on an entity, empty slots included, in the range's order.
// vanilla: SlotProvider.getSlotsFromRange
func entitySlotAccess(e Entity, name string) []rules.SlotAccess {
	slots, _ := itemSlots(name)
	var out []rules.SlotAccess
	for _, id := range slots {
		if _, ok := e.ItemIn(id); ok {
			slot := id
			out = append(out, rules.SlotAccess{
				Get: func() protocol.ItemStack {
					stack, _ := e.ItemIn(slot)
					return stack
				},
				Set: func(stack protocol.ItemStack) bool {
					return e.SetItemIn(slot, stack)
				},
			})
		}
	}
	return out
}

// blockSlotAccess returns read/write access to each slot a range word names on
// a container block at (x, y, z) of the given size.
// vanilla: SlotProvider.getSlotsFromRange
func blockSlotAccess(src Source, x, y, z int32, size int, name string) []rules.SlotAccess {
	slots, _ := itemSlots(name)
	var out []rules.SlotAccess
	for _, id := range slots {
		if int(id) >= 0 && int(id) < size {
			slot := int(id)
			out = append(out, rules.SlotAccess{
				Get: func() protocol.ItemStack {
					return src.ItemInContainerAt(x, y, z, slot)
				},
				Set: func(stack protocol.ItemStack) bool {
					src.SetItemInContainerAt(x, y, z, slot, stack)
					return true
				},
			})
		}
	}
	return out
}
