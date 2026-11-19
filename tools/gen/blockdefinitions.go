package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

var definitionLine = regexp.MustCompile(`(?m)DEFINITION (minecraft:[a-z0-9_]+) ([A-Za-z0-9]+) (\S+)$`)

// blockDefinitions fills the class information omitted from the 26.3 block report.
func blockDefinitions(e *env, blocks vanilla.Blocks) (map[string]vanilla.BlockDefinition, error) {
	definitions := map[string]vanilla.BlockDefinition{}
	for name, block := range blocks.All() {
		if block.Definition.Type != "" {
			definitions[name] = block.Definition
		}
	}
	if len(definitions) == blocks.Len() {
		return definitions, nil
	}
	if len(definitions) != 0 {
		return nil, fmt.Errorf("only %d of %d blocks have definitions", len(definitions), blocks.Len())
	}
	out, err := e.java("BlockDefinitions", vanilla.JavaOptions{})
	if err != nil {
		return nil, err
	}
	return parseBlockDefinitions(blocks, out)
}

func parseBlockDefinitions(blocks vanilla.Blocks, out string) (map[string]vanilla.BlockDefinition, error) {
	definitions := map[string]vanilla.BlockDefinition{}
	for _, m := range definitionLine.FindAllStringSubmatch(out, -1) {
		name := m[1]
		if _, ok := blocks.Get(name); !ok {
			return nil, fmt.Errorf("block class dump names %s, absent from the block report", name)
		}
		if _, duplicate := definitions[name]; duplicate {
			return nil, fmt.Errorf("duplicate block definition: %s", name)
		}
		definition := vanilla.BlockDefinition{Type: blockClassType(m[2])}
		if m[3] != "-" {
			if _, ok := blocks.Get(m[3]); !ok || definition.Type != "minecraft:concrete_powder" {
				return nil, fmt.Errorf("unsupported concrete target: %s -> %s", name, m[3])
			}
			definition.Concrete = new(m[3])
		}
		definitions[name] = definition
	}
	if len(definitions) != blocks.Len() {
		return nil, fmt.Errorf("%d block classes for %d report entries", len(definitions), blocks.Len())
	}
	return definitions, nil
}

// blockClassType preserves the codec labels consumed by the block rules.
func blockClassType(class string) string {
	aliases := map[string]string{
		"SnowyBlock": "snowy_dirt", "RedStoneWireBlock": "redstone_wire", "RedStoneOreBlock": "redstone_ore",
		"CarvedPumpkinBlock": "jack_o_lantern", "TrapDoorBlock": "trapdoor",
		"WeatheringCopperBarsBlock": "weathering_copper_bar", "EnchantingTableBlock": "enchantment_table",
		"TripWireBlock": "tripwire", "PiglinWallSkullBlock": "piglinwallskull",
		"WeatheringCopperTrapDoorBlock": "weathering_copper_trapdoor", "CopperBulbBlock": "copper_bulb_block",
		// These classes replace or extend the existing families in 26.3.
		"LeavesBlock": "untinted_particle_leaves", "PathBlock": "dirt_path", "StrawBedBlock": "bed",
	}
	if alias, ok := aliases[class]; ok {
		return "minecraft:" + alias
	}
	stem := strings.TrimSuffix(class, "Block")
	if stem == "" {
		stem = "Block"
	}
	var result strings.Builder
	for i, r := range stem {
		if i > 0 && unicode.IsUpper(r) {
			result.WriteByte('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return "minecraft:" + result.String()
}
