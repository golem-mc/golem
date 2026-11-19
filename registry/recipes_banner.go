package registry

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// The banner special crafting recipes: a banner decorating a shield and a
// banner copied onto a blank one. Their results depend on the grid, so the
// data pack carries none.

var shieldItem = MustItem("minecraft:shield")

// CraftRemainders returns the stack each grid cell leaves behind when a
// craft spends one of it, index for index with the cells: the item's own
// crafting remainder, and for a banner duplicate or book clone the source
// too.
// vanilla: CraftingRecipe.getRemainingItems, BannerDuplicateRecipe.getRemainingItems,
// BookCloningRecipe.getRemainingItems
func CraftRemainders(cells []protocol.ItemStack) []protocol.ItemStack {
	remainders := make([]protocol.ItemStack, len(cells))
	_, sourceCell, duplicate := bannerDuplicate(cells)
	if _, cloneCell, cloning := bookClone(cells); cloning {
		sourceCell, duplicate = cloneCell, true
	}
	for i, cell := range cells {
		if cell.Empty() {
			continue
		}
		if remainder := CraftingRemainder(cell.ItemID); !remainder.Empty() {
			remainders[i] = remainder
			continue
		}
		if duplicate && i == sourceCell {
			remainders[i] = cell
			remainders[i].Count = 1
		}
	}
	return remainders
}

// bannerPatternCount is how many layers the stack's banner_patterns holds.
func bannerPatternCount(stack protocol.ItemStack) int {
	layers, _ := stack.BannerPatternsOf()
	return len(layers)
}

// bannerColor returns the dye of a banner item, false for any other item.
func bannerColor(item int32) (int32, bool) {
	name, ok := ItemName(item)
	if !ok {
		return 0, false
	}
	colour, ok := strings.CutSuffix(strings.TrimPrefix(name, "minecraft:"), "_banner")
	if !ok {
		return 0, false
	}
	dye := slices.Index(DyeNames, colour)
	return int32(dye), dye >= 0
}

// gridPair returns the indices of the grid's two non-empty cells, in grid
// order; ok is false unless exactly two are filled.
func gridPair(cells []protocol.ItemStack) (first, second int, ok bool) {
	filled := 0
	for i := range cells {
		if cells[i].Empty() {
			continue
		}
		switch filled {
		case 0:
			first = i
		case 1:
			second = i
		default:
			return 0, 0, false
		}
		filled++
	}
	return first, second, filled == 2
}

// shieldDecoration matches a banner beside a shield without patterns: the
// shield carries the banner's patterns and its colour as the base.
// vanilla: ShieldDecorationRecipe
func shieldDecoration(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	i, j, ok := gridPair(cells)
	if !ok {
		return protocol.ItemStack{}, false
	}
	banner, shield := cells[i], cells[j]
	if _, ok := bannerColor(banner.ItemID); !ok {
		banner, shield = shield, banner
	}
	dye, ok := bannerColor(banner.ItemID)
	if !ok || shield.ItemID != shieldItem || bannerPatternCount(shield) > 0 {
		return protocol.ItemStack{}, false
	}
	result := shield
	result.Count = 1
	// An explicit empty list equals the shield's default, which vanilla's
	// component patch drops.
	if layers, _ := banner.BannerPatternsOf(); len(layers) > 0 {
		result.SetBannerPatterns(layers)
	}
	result.SetBaseColor(dye)
	return result, true
}

// bannerDuplicateResult is bannerDuplicate in the shape specialCraftResult
// tries.
func bannerDuplicateResult(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	source, _, ok := bannerDuplicate(cells)
	return source, ok
}

// bannerDuplicate matches two banners of one colour, one with patterns and
// one blank: the result is the patterned one, and sourceCell its index.
// vanilla: BannerDuplicateRecipe
func bannerDuplicate(cells []protocol.ItemStack) (source protocol.ItemStack, sourceCell int, ok bool) {
	i, j, ok := gridPair(cells)
	if !ok {
		return protocol.ItemStack{}, 0, false
	}
	stacks, at := [2]protocol.ItemStack{cells[i], cells[j]}, [2]int{i, j}
	first, second := bannerPatternCount(stacks[0]), bannerPatternCount(stacks[1])
	if _, banner := bannerColor(stacks[0].ItemID); !banner ||
		stacks[0].ItemID != stacks[1].ItemID ||
		first > 6 || second > 6 || (first > 0) == (second > 0) {
		return protocol.ItemStack{}, 0, false
	}
	which := 0
	if second > 0 {
		which = 1
	}
	source = stacks[which]
	source.Count = 1
	return source, at[which], true
}
