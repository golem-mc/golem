package command

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() { rules.SetComponentPatch(patchComponent) }

// The refusals of an item modifier argument: a name no modifier answers to,
// and an inline modifier its codec refuses.
// vanilla: ResourceOrIdArgument
const (
	modifierNotFoundKey    = "argument.resource_or_id.no_such_element"
	modifierFailedParseKey = "argument.resource_or_id.failed_to_parse"
)

// parseItemModifier reads an item modifier argument: an identifier names a
// modifier a data pack defines, and anything else is an inline modifier's
// SNBT. The refusals stand at the argument's end.
// vanilla: ResourceOrIdArgument.parse
func parseItemModifier(text string) (registry.LootFunction, *ParseError) {
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		if fn, found := registry.FindItemModifier(registry.Qualify(name)); found {
			return fn, nil
		}
		return nil, &ParseError{
			Key:    modifierNotFoundKey,
			With:   []string{registry.Qualify(name), "minecraft:item_modifier"},
			Cursor: len(text),
		}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		span := joinSelector([]Token{{Text: text}})
		return nil, snbtRefusal(pe, span, pe.Offset)
	}
	if n != len(text) {
		return nil, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	fn, msg := decodeItemModifier(tag)
	if msg != "" {
		return nil, &ParseError{Key: modifierFailedParseKey, With: []string{msg}, Cursor: len(text)}
	}
	return fn, nil
}

// checkItemModifierArgument holds the modifier argument to parseItemModifier
// while the walk reads it.
func checkItemModifierArgument(text string) *ParseError {
	_, err := parseItemModifier(text)
	return err
}

// decodeItemModifier reads an inline modifier: one typed function, or a
// list of them run in order. The message is the codec's refusal, empty for
// none.
// vanilla: LootItemFunctions.DIRECT_CODEC
func decodeItemModifier(tag nbt.Tag) (registry.LootFunction, string) {
	switch tag.(type) {
	case nbt.Compound:
		fn, err := datapack.DecodeItemModifierValue(nbtJSON(tag))
		if err != nil {
			msg := inlineLootError(err)
			// The holder set also tries this map as a single typed holder.
			// vanilla: HolderSetCodec.directCodec, ExtraCodecs.compactListCodec
			return nil, "Failed to parse either. First: " + msg +
				"; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: " + nbt.Stringify(tag) +
				"; Second: " + msg
		}
		return fn, ""
	case nbt.List:
		fn, err := datapack.DecodeItemModifierValue(nbtJSON(tag))
		if err != nil {
			return nil, inlineLootError(err)
		}
		// A name in the list resolves while the argument parses.
		// vanilla: RegistryFileCodec.decode
		if seq, ok := fn.(registry.LootFunctionSequence); ok {
			for _, f := range seq.Functions {
				if ref, ok := f.(registry.LootFunctionReference); ok {
					if _, found := registry.FindItemModifier(ref.Name); !found {
						return nil, "Failed to get element " + ref.Name + " from registry minecraft:item_modifier"
					}
				}
			}
		}
		return fn, ""
	}
	text := nbt.Stringify(tag)
	return nil, "Failed to parse either. First: Not a map: " + text + "; Second: Not a list: " + text
}

// inlineLootError words the codec's refusal of a function type this server
// does not model the way the inline command reports it.
func inlineLootError(err error) string {
	if kind, ok := strings.CutPrefix(err.Error(), "function "); ok && strings.HasSuffix(kind, " is not supported by this server") {
		return "Loot function " + kind
	}
	return err.Error()
}

// nbtJSON turns a tag into the shapes the data-pack loot decoder reads: a
// byte of 0 or 1 is a boolean and every other number a json.Number, so the
// SNBT a component keeps loses its type suffixes.
func nbtJSON(tag nbt.Tag) any {
	switch v := tag.(type) {
	case nbt.Compound:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = nbtJSON(e)
		}
		return out
	case nbt.List:
		out := make([]any, len(v.Items))
		for i, e := range v.Items {
			out[i] = nbtJSON(nbt.ListElement(e))
		}
		return out
	case nbt.String:
		return string(v)
	case nbt.Byte:
		if v == 0 || v == 1 {
			return v == 1
		}
	case nbt.Long:
		return json.Number(strconv.FormatInt(int64(v), 10)) // exact, as a set_loot_table seed needs
	}
	if f, ok := nbt.NumberValue(tag); ok {
		return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return nbt.Stringify(tag)
}

// patchComponent sets one component of a stack from its SNBT value, the
// old value cleared first so that a default value clears it, or removes it;
// replacements keep their patch position, resets to defaults forget it.
// false is a component this server cannot carry or clear.
// vanilla: PatchedDataComponentMap.applyPatch
func patchComponent(stack *protocol.ItemStack, name, value string, remove bool) bool {
	if component, kept := protocol.RemovableType(name); kept && remove {
		if registry.ItemHasComponentDefault(stack.ItemID, name) {
			return stack.Remove(component)
		}
		stack.ResetComponent(component)
		return true
	}
	switch name {
	case "minecraft:damage":
		stack.Damage = 0
	case "minecraft:unbreakable":
		stack.Unbreakable = false
	case "minecraft:custom_name":
		stack.CustomName = nil
	case "minecraft:enchantments":
		if remove {
			stack.SetEnchantments(false, nil)
		}
	case "minecraft:stored_enchantments":
		if remove {
			stack.SetEnchantments(true, nil)
		}
	case "minecraft:repair_cost":
		stack.RepairCost = 0
	case "minecraft:enchantment_glint_override":
		stack.GlintOverride = protocol.GlintUnset
	default:
		return false
	}
	component, _ := registry.ComponentID(name)
	if remove {
		stack.ForgetPatch(component)
		return true
	}
	if !applyComponent(stack, stack.ItemID, name, value) {
		return false
	}
	present := true
	switch component {
	case protocol.DataComponentDamage:
		present = stack.Damage > 0
	case protocol.DataComponentRepairCost:
		present = stack.RepairCost > 0
	case protocol.DataComponentEnchantmentGlintOverride:
		present = stack.GlintOverride != protocol.GlintUnset
	case protocol.DataComponentEnchantments:
		present = len(stack.Enchantments) > 0
	case protocol.DataComponentStoredEnchantments:
		present = len(stack.StoredEnchantments) > 0
	}
	if present {
		stack.NotePatch(component)
	} else {
		stack.ForgetPatch(component)
	}
	return true
}
