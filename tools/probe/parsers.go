package main

import (
	"fmt"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// commandParserNames maps a parser ID to its name. The client has the
// minecraft:command_argument_type registry built in, so the IDs come from
// the vanilla data report (reference-server/generated/reports/registries.json,
// entries → protocol_id). See tools/README.md to re-read them.
var commandParserNames = map[int32]string{
	0:  "brigadier:bool",
	1:  "brigadier:float",
	2:  "brigadier:double",
	3:  "brigadier:integer",
	4:  "brigadier:long",
	5:  "brigadier:string",
	6:  "minecraft:entity",
	7:  "minecraft:game_profile",
	8:  "minecraft:block_pos",
	9:  "minecraft:column_pos",
	10: "minecraft:vec3",
	11: "minecraft:vec2",
	12: "minecraft:block_state",
	13: "minecraft:block_predicate",
	14: "minecraft:item_stack",
	15: "minecraft:item_predicate",
	16: "minecraft:team_color",
	17: "minecraft:hex_color",
	18: "minecraft:component",
	19: "minecraft:style",
	20: "minecraft:message",
	21: "minecraft:nbt_compound_tag",
	22: "minecraft:nbt_tag",
	23: "minecraft:nbt_path",
	24: "minecraft:objective",
	25: "minecraft:objective_criteria",
	26: "minecraft:operation",
	27: "minecraft:particle",
	28: "minecraft:angle",
	29: "minecraft:rotation",
	30: "minecraft:scoreboard_slot",
	31: "minecraft:score_holder",
	32: "minecraft:swizzle",
	33: "minecraft:team",
	34: "minecraft:item_slot",
	35: "minecraft:item_slots",
	36: "minecraft:resource_location",
	37: "minecraft:function",
	38: "minecraft:entity_anchor",
	39: "minecraft:int_range",
	40: "minecraft:float_range",
	41: "minecraft:dimension",
	42: "minecraft:gamemode",
	43: "minecraft:time",
	44: "minecraft:resource_or_tag",
	45: "minecraft:resource_or_tag_key",
	46: "minecraft:resource",
	47: "minecraft:resource_key",
	48: "minecraft:resource_selector",
	49: "minecraft:template_mirror",
	50: "minecraft:template_rotation",
	51: "minecraft:heightmap",
	52: "minecraft:loot_table",
	53: "minecraft:loot_predicate",
	54: "minecraft:loot_modifier",
	55: "minecraft:context_float_provider",
	56: "minecraft:context_int_provider",
	57: "minecraft:slot_source",
	58: "minecraft:dialog",
	59: "minecraft:feature",
	60: "minecraft:swing_animation",
	61: "minecraft:uuid",
}

// brigadierStringModes names the parsing behaviors the brigadier:string
// parser's mode selects from.
var brigadierStringModes = map[int32]string{
	0: "SINGLE_WORD",
	1: "QUOTABLE_PHRASE",
	2: "GREEDY_PHRASE",
}

// readParserProperties describes one argument node's parser properties
// (empty when it has none), reporting whether the parser is known. An unknown
// one ends the packet: its properties have no known end.
// vanilla: ClientboundCommandsPacket
func readParserProperties(r *protocol.Reader, id int32) (string, bool) {
	switch id {
	case 1: // brigadier:float
		return describeNumberRange(r, r.ReadF32), true
	case 2: // brigadier:double
		return describeNumberRange(r, r.ReadF64), true
	case 3: // brigadier:integer
		return describeNumberRange(r, r.ReadI32), true
	case 4: // brigadier:long
		return describeNumberRange(r, r.ReadI64), true
	case 5: // brigadier:string
		mode := r.ReadVarInt()
		if name, ok := brigadierStringModes[mode]; ok {
			return "mode=" + name, true
		}
		return fmt.Sprintf("mode=%d", mode), true
	case 6, 31: // minecraft:entity, minecraft:score_holder
		return fmt.Sprintf("flags=%#02x", r.ReadU8()), true
	case 43: // minecraft:time
		return fmt.Sprintf("min=%d", r.ReadI32()), true
	case 44, 45, 46, 47, 48: // minecraft:resource_or_tag, resource_or_tag_key,
		// resource, resource_key, resource_selector — each carries the
		// identifier of the registry its values are drawn from.
		return "registry=" + r.ReadString(32767), true
	default:
		if _, named := commandParserNames[id]; named {
			return "", true // a known parser with no properties
		}
		return "", false
	}
}

// describeNumberRange reads the four brigadier numeric parsers' shape: a
// flags byte whose low bits say which bounds follow, at the parser's width.
// Absent bounds are not described.
func describeNumberRange[V int32 | int64 | float32 | float64](r *protocol.Reader, read func() V) string {
	flags := r.ReadU8()
	var parts []string
	if flags&0x01 != 0 {
		parts = append(parts, fmt.Sprintf("min=%v", read()))
	}
	if flags&0x02 != 0 {
		parts = append(parts, fmt.Sprintf("max=%v", read()))
	}
	return strings.Join(parts, " ")
}
