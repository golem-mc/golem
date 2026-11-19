package rules

import "github.com/golem-mc/golem/registry"

var (
	obsidianState = registry.MustState("minecraft:obsidian", nil)
	bedrockState  = registry.MustState("minecraft:bedrock", nil)
)

// EndCrystalBase reports whether an end crystal may be placed on the block.
// vanilla: EndCrystalItem.useOn
func EndCrystalBase(state uint16) bool {
	return state == obsidianState || state == bedrockState
}
