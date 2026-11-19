package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// leavesBlocks marks the leaf blocks.
var leavesBlocks = registry.BlockTagOf(registry.TagLeaves)

// infestedBlocks marks the infested_* blocks.
var infestedBlocks = registry.BlocksOf(rules.IsInfestedBlock)

// railBlocks marks the blocks of the rails tag.
var railBlocks = registry.BlockTagOf("minecraft:rails")

// visuallyEmptyBlocks marks the blocks whose visual shape is empty though
// they collide: glass, grates, bars, panes and powder snow.
// vanilla: TransparentBlock, IronBarsBlock, PowderSnowBlock .getVisualShape
var visuallyEmptyBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b.Type() {
	case registry.BlockTypeTransparent, registry.BlockTypeStainedGlass, registry.BlockTypeTintedGlass,
		registry.BlockTypeWaterloggedTransparent, registry.BlockTypeWeatheringCopperGrate, registry.BlockTypePowderSnow:
		return true
	}
	return b.InGroup(registry.GroupBars)
})
