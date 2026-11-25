package command

import (
	"errors"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// lootTableRef is the table a /loot rolls: a registered one by name, or an
// inline one the command carried.
type lootTableRef struct {
	name   string
	inline *registry.LootTable
}

// parseLootTable reads a loot table argument: an identifier names a table,
// anything else is an inline table's SNBT. The refusals stand at the
// argument's end.
// vanilla: ResourceOrIdArgument.parse
func parseLootTable(text string) (lootTableRef, *ParseError) {
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		name = registry.Qualify(name)
		if !lootTableKnown(name) {
			return lootTableRef{}, &ParseError{
				Key:    lootTableNotFoundKey,
				With:   []string{name, lootTableRegistry},
				Cursor: len(text),
			}
		}
		return lootTableRef{name: name}, nil
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		span := joinSelector([]Token{{Text: text}})
		return lootTableRef{}, snbtRefusal(pe, span, pe.Offset)
	}
	if n != len(text) {
		return lootTableRef{}, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	table, msg, err := decodeLootTable(tag)
	if err != nil {
		return lootTableRef{}, weightOverflowError(err, len(text))
	}
	if msg != "" {
		return lootTableRef{}, &ParseError{Key: modifierFailedParseKey, With: []string{msg}, Cursor: len(text)}
	}
	return lootTableRef{name: inlineLootTableName, inline: &table}, nil
}

// inlineLootTableName stands for an inline table where a roll needs the
// table's own name; no registered table has it.
const inlineLootTableName = "minecraft:inline"

// lootTableKnown says whether the server rolls a table by the name.
func lootTableKnown(name string) bool {
	_, ok := registry.FindLootTable(name)
	return ok
}

// decodeLootTable reads an inline loot table with the data-pack codec. The
// message is the codec's refusal, empty for none; the error is
// datapack.ErrWeightOverflow, which is no codec refusal.
// vanilla: LootTable.DIRECT_CODEC
func decodeLootTable(tag nbt.Tag) (registry.LootTable, string, error) {
	table, err := datapack.DecodeLootTableValue(nbtJSON(tag))
	if errors.Is(err, datapack.ErrWeightOverflow) {
		return registry.LootTable{}, "", err
	}
	if err != nil {
		return registry.LootTable{}, inlineLootError(err), nil
	}
	if msg := unknownLootReference(tag); msg != "" {
		return registry.LootTable{}, msg, nil
	}
	return table, "", nil
}

// unknownLootReference is the refusal of the first loot_table entry naming a
// table the server does not have, empty for none. The data-pack codec reads
// the name as a key because packs load their tables in any order; a command
// resolves it against the loaded registry.
// vanilla: NestedLootTable.CODEC (LootTable.LIST_CODEC), HolderSetCodec, RegistryFileCodec.decode
func unknownLootReference(tag nbt.Tag) string {
	c, ok := tag.(nbt.Compound)
	if !ok {
		return ""
	}
	if typ, _ := c["type"].(nbt.String); registry.Qualify(string(typ)) == "minecraft:loot_table" {
		if value, ok := c["value"].(nbt.String); ok {
			if name := registry.Qualify(string(value)); !lootTableKnown(name) {
				return "Failed to parse either. First: Not a tag id; Second: Failed to parse either. First: Not a list: " +
					nbt.Stringify(value) + "; Second: Failed to get element " + name + " from registry minecraft:loot_table"
			}
		}
	}
	for _, key := range []string{"pools", "entries", "children"} {
		list, _ := c[key].(nbt.List)
		for _, e := range list.Items {
			if msg := unknownLootReference(e); msg != "" {
				return msg
			}
		}
	}
	return ""
}
