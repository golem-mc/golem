package registry

import "github.com/golem-mc/golem/internal/protocol"

// StackTrimMaterial returns the trim_material registry ID the stack provides:
// its own provides_trim_material, none once removed, else its item's default.
// An inline material has no registry ID and counts as none.
// vanilla: SmithingTrimRecipe.applyTrim, DataComponents.PROVIDES_TRIM_MATERIAL
func StackTrimMaterial(stack protocol.ItemStack) (int32, bool) {
	if stack.Empty() || stack.Removes(protocol.DataComponentProvidesTrimMaterial) {
		return 0, false
	}
	if data, ok := propBytes(stack, protocol.DataComponentProvidesTrimMaterial); ok {
		r := protocol.NewReader(data)
		id := r.ReadVarInt() - 1 // 0 is an inline holder
		return id, r.Err() == nil && id >= 0
	}
	id, ok := TrimMaterials[stack.ItemID]
	return id, ok
}

// StackBannerPatterns returns the banner_pattern registry IDs the stack
// provides: its own provides_banner_patterns, none once removed, else its
// item's default. The bool is false when the stack provides no set at all.
// vanilla: LoomMenu.getSelectablePatterns, DataComponents.PROVIDES_BANNER_PATTERNS
func StackBannerPatterns(stack protocol.ItemStack) ([]int32, bool) {
	if stack.Empty() || stack.Removes(protocol.DataComponentProvidesBannerPatterns) {
		return nil, false
	}
	data, ok := propBytes(stack, protocol.DataComponentProvidesBannerPatterns)
	if !ok {
		ids := PatternItemPatterns(stack.ItemID)
		return ids, ids != nil
	}
	r := protocol.NewReader(data)
	n := r.ReadVarInt()
	if n == 0 {
		return BannerPatternTagIDs(r.ReadString(32767)), r.Err() == nil
	}
	ids := make([]int32, 0, n-1)
	for range n - 1 {
		ids = append(ids, r.ReadVarInt())
	}
	return ids, r.Err() == nil
}
