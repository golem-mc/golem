package registry

import "slices"

// IsBannerItem reports whether the item is one of the sixteen banners.
// vanilla: BannerItem
func IsBannerItem(item int32) bool {
	switch Item(item) {
	case ItemWhiteBanner, ItemOrangeBanner, ItemMagentaBanner, ItemLightBlueBanner,
		ItemYellowBanner, ItemLimeBanner, ItemPinkBanner, ItemGrayBanner,
		ItemLightGrayBanner, ItemCyanBanner, ItemPurpleBanner, ItemBlueBanner,
		ItemBrownBanner, ItemGreenBanner, ItemRedBanner, ItemBlackBanner:
		return true
	}
	return false
}

// itemDyeColours is the DyeNames index of each item's minecraft:dye
// component, -1 for an item without one.
var itemDyeColours = func() (t [ItemCount]int8) {
	for i := range t {
		t[i] = -1
	}
	for item, name := range itemDyeColourNames {
		t[item] = int8(slices.Index(DyeNames, name))
	}
	return t
}()

// DyeColour returns the DyeNames index of the item's minecraft:dye
// component, false for an item without one or an ID outside the registry.
// vanilla: DataComponents.DYE
func (i Item) DyeColour() (int32, bool) {
	if i < 0 || i >= ItemCount {
		return 0, false
	}
	c := itemDyeColours[i]
	return int32(c), c >= 0
}

// syncTagIDs resolves every tag of the synchronized registry named to its
// members' registry IDs, in the tag's own order (the loom's client lists
// banner patterns in it, and Painting.create's roll indexes it).
func syncTagIDs(sync []TaggedRegistry, registry string) map[string][]int32 {
	m := make(map[string][]int32)
	reg, _ := Get(registry)
	for _, r := range sync {
		if r.Registry != registry {
			continue
		}
		for _, tag := range r.Tags {
			ids := make([]int32, 0, len(tag.Entries))
			for _, name := range tag.Entries {
				if id := reg.ID(name); id >= 0 {
					ids = append(ids, id)
				}
			}
			m[tag.Name] = ids
		}
	}
	return m
}

// BannerPatternTagIDs returns the banner_pattern registry IDs a tag names,
// in the tag's order.
func BannerPatternTagIDs(tag string) []int32 { return tagsNow.Load().bannerPatternIDs[tag] }

// PaintingVariantTagIDs returns the painting_variant registry IDs a tag
// names, data packs included, in the tag's order.
func PaintingVariantTagIDs(tag string) []int32 { return tagsNow.Load().paintingVariantIDs[tag] }
