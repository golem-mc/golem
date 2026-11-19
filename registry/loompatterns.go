package registry

// PatternItemPatterns returns the banner patterns a loom pattern item
// provides, nil for an item that provides none.
// vanilla: DataComponents.PROVIDES_BANNER_PATTERNS
func PatternItemPatterns(item int32) []int32 {
	tag, ok := bannerPatternItemTags[Item(item)]
	if !ok {
		return nil
	}
	return BannerPatternTagIDs(tag)
}
