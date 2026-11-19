package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// parseSlotSource reads a slot source argument: a slot range word, a data
// pack source's id, or an inline source's SNBT. The refusals stand at the
// argument's end.
// vanilla: SlotSourceArgument.parse
func parseSlotSource(text string) (registry.SlotSource, *ParseError) {
	if _, known := itemSlotIDs[text]; known {
		return registry.SlotRangeSource{Source: "container", Slots: text}, nil
	}
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		if source, found := registry.FindSlotSource(registry.Qualify(name)); found {
			return source, nil
		}
		return nil, &ParseError{
			Key:    modifierNotFoundKey,
			With:   []string{registry.Qualify(name), "minecraft:slot_source"},
			Cursor: len(text),
		}
	}
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return nil, &ParseError{Key: slotUnknownKey, With: []string{text}, Cursor: len(text)}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		return nil, snbtRefusal(pe, joinSelector([]Token{{Text: text}}), pe.Offset)
	}
	if n != len(text) {
		return nil, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	source, err := datapack.DecodeSlotSourceValue(nbtJSON(tag))
	if err != nil {
		return nil, &ParseError{Key: modifierFailedParseKey, With: []string{err.Error()}, Cursor: len(text)}
	}
	return source, nil
}

// checkSlotSourceArgument holds the argument to parseSlotSource while the
// walk reads it.
func checkSlotSourceArgument(text string) *ParseError {
	_, err := parseSlotSource(text)
	return err
}

// slotSourceStacks is the stacks a source finds on one holder of slots: the
// range sources' "container" is the holder and "this" the command's runner.
// vanilla: ItemCommands.getSlotsFromProvider
func slotSourceStacks(src Source, source registry.SlotSource, container func(name string) []protocol.ItemStack) []protocol.ItemStack {
	ctx := rules.LootContext{ContainerSlots: container}
	if self := src.SelfEntity(); self != nil {
		ctx.This = lootEntityOf(self)
	}
	return rules.SlotSourceStacks(source, &ctx)
}

// entitySlotStacks reads the stack in each slot a range word names on an
// entity, empty slots included, in the range's order.
// vanilla: SlotProvider.getSlotsFromRange
func entitySlotStacks(e Entity, name string) []protocol.ItemStack {
	slots, _ := itemSlots(name)
	var stacks []protocol.ItemStack
	for _, slot := range slots {
		if stack, ok := e.ItemIn(slot); ok {
			stacks = append(stacks, stack)
		}
	}
	return stacks
}
