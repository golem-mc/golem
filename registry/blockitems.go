package registry

import "strings"

// wallVariantTypes are the definition types of wall-placed variants, named
// with "wall_" inserted (wall_torch, oak_wall_sign), which drop their
// standing block's loot.
// vanilla: StandingAndWallBlockItem, WallBannerBlock, WallSkullBlock, CoralWallFanBlock
var wallVariantTypes = map[string]bool{
	"minecraft:wall_torch":          true,
	"minecraft:redstone_wall_torch": true,
	"minecraft:wall_sign":           true,
	"minecraft:wall_hanging_sign":   true,
	"minecraft:wall_banner":         true,
	"minecraft:wall_skull":          true,
	"minecraft:wither_wall_skull":   true,
	"minecraft:piglinwallskull":     true,
	"minecraft:player_wall_head":    true,
	"minecraft:coral_wall_fan":      true,
	"minecraft:base_coral_wall_fan": true,
}

var wallVariants, standingVariants = func() (map[string]string, map[string]string) {
	wall, standing := map[string]string{}, map[string]string{}
	for name, typ := range BlockTypes {
		if !wallVariantTypes[typ] {
			continue
		}
		s := strings.Replace(name, "wall_", "", 1)
		if _, ok := BlockTypes[s]; ok {
			wall[s], standing[name] = name, s
		}
	}
	return wall, standing
}()

// wallVariantOf holds WallVariant's answer per block, the wall block plus one.
var wallVariantOf = blockPairs(wallVariants)

// WallVariant is the block a standing block's item places against a wall.
func (b Block) WallVariant() (Block, bool) { return pairOf(wallVariantOf, b) }

// StandingVariant names the standing block of a wall variant.
func StandingVariant(name string) (string, bool) {
	s, ok := standingVariants[name]
	return s, ok
}

// BlockOfItem names the block an item places: its own name, or for the
// items above the block they plant.
func BlockOfItem(item string) string {
	if b, ok := itemBlocks[item]; ok {
		return b
	}
	return item
}

// itemPlacesBlock and itemBlock hold Item.Block's answer per item;
// blockItem holds Block.Item's answer per block, the item plus one.
var itemPlacesBlock, itemBlock, blockItem = func() ([ItemCount]bool, [ItemCount]Block, [BlockCount]Item) {
	var ok [ItemCount]bool
	var blocks [ItemCount]Block
	var items [BlockCount]Item
	for i, name := range Items {
		if b, found := BlockByName(BlockOfItem(name)); found {
			ok[i], blocks[i] = true, b
		}
		if b, found := BlockByName(name); found {
			items[b] = Item(i) + 1
		}
	}
	return ok, blocks, items
}()

// Item returns the item with the block's own name, and false for a block
// without one.
func (b Block) Item() (Item, bool) {
	if b >= BlockCount || blockItem[b] == 0 {
		return 0, false
	}
	return blockItem[b] - 1, true
}

// Block returns the block the item places, and false for an item that places
// none or an ID outside the registry.
func (i Item) Block() (Block, bool) {
	if i < 0 || i >= ItemCount || !itemPlacesBlock[i] {
		return 0, false
	}
	return itemBlock[i], true
}

// CloneItem returns the item pick block hands out for a state: often not the
// block's own name (redstone wire's redstone, a crop's seeds), and none for
// some (water, moving piston). vanilla: BlockState.getCloneItemStack
func CloneItem(state uint16) (int32, bool) {
	name, props, ok := BlockStateName(state)
	if !ok {
		return 0, false
	}
	item, ok := CloneItemStates[name+"|"+props]
	if !ok {
		item, ok = CloneItems[name]
	}
	if !ok || item == "minecraft:air" {
		return 0, false
	}
	return ItemID(item)
}

// cloneBlockStateProps are the blocks whose pick copies a state property
// into block_state: a light's level, a test block's mode, a copper golem
// statue's pose, and a bee nest's honey only when block data was asked for.
// vanilla: LightBlock.getCloneItemStack, BeehiveBlock.getCloneItemStack,
// TestBlock.getCloneItemStack, CopperGolemStatueBlockEntity.getItem
var cloneBlockStateProps = map[string]struct {
	prop         string
	onlyWithData bool
}{
	"minecraft:light":    {prop: "level"},
	"minecraft:beehive":  {prop: "honey_level", onlyWithData: true},
	"minecraft:bee_nest": {prop: "honey_level", onlyWithData: true},

	"minecraft:test_block": {prop: "mode"},

	"minecraft:copper_golem_statue":                 {prop: "copper_golem_pose"},
	"minecraft:exposed_copper_golem_statue":         {prop: "copper_golem_pose"},
	"minecraft:weathered_copper_golem_statue":       {prop: "copper_golem_pose"},
	"minecraft:oxidized_copper_golem_statue":        {prop: "copper_golem_pose"},
	"minecraft:waxed_copper_golem_statue":           {prop: "copper_golem_pose"},
	"minecraft:waxed_exposed_copper_golem_statue":   {prop: "copper_golem_pose"},
	"minecraft:waxed_weathered_copper_golem_statue": {prop: "copper_golem_pose"},
	"minecraft:waxed_oxidized_copper_golem_statue":  {prop: "copper_golem_pose"},
}

// CloneItemStateProps returns the block_state properties pick block copies
// for state, and whether any; a bee nest only with includeData.
func CloneItemStateProps(state uint16, includeData bool) (map[string]string, bool) {
	name, raw, ok := BlockStateName(state)
	if !ok {
		return nil, false
	}
	entry, ok := cloneBlockStateProps[name]
	if !ok || (entry.onlyWithData && !includeData) {
		return nil, false
	}
	value, ok := ParseStateProps(raw)[entry.prop]
	if !ok {
		return nil, false
	}
	return map[string]string{entry.prop: value}, true
}

var skullTypes = map[string]bool{
	"minecraft:skull":             true,
	"minecraft:wall_skull":        true,
	"minecraft:wither_skull":      true,
	"minecraft:wither_wall_skull": true,
	"minecraft:piglinwallskull":   true,
	"minecraft:player_head":       true,
	"minecraft:player_wall_head":  true,
}

var skullBlock = BlocksWhere(func(name string) bool { return skullTypes[BlockTypes[name]] })

// IsSkullBlockState is Block.IsSkull for a state ID.
func IsSkullBlockState(state uint16) bool {
	return int(state) < StateCount && skullBlock[blockOfState[state]]
}

// IsSkull reports whether a block is an AbstractSkullBlock: any head or skull,
// floor or wall.
func (b Block) IsSkull() bool { return b < BlockCount && skullBlock[b] }

// shulkerBoxItems marks the items of the built-in minecraft:shulker_boxes
// tag, read once so a data pack replacing the tag changes nothing.
var shulkerBoxItems = func() (t [ItemCount]bool) {
	for i := range Item(ItemCount) {
		t[i] = i.InTag(ItemTagIDShulkerBoxes)
	}
	return t
}()

// IsShulkerBox reports whether the item places a shulker box, for the
// checks vanilla makes with `block instanceof ShulkerBoxBlock`.
// vanilla: BlockItem.canFitInsideContainerItems
func (i Item) IsShulkerBox() bool {
	return i >= 0 && i < ItemCount && shulkerBoxItems[i]
}
