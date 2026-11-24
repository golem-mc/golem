package protocol

import "slices"

// The minecraft:bundle_contents component is one of the unmodelled ones this
// server keeps as the bytes it arrived in, but the click path has to fill and
// empty a bundle, so its value is decoded and written here.

// BundleContentsOf returns the items a stack's minecraft:bundle_contents patch
// holds; false when the component is absent from the patch.
func (s ItemStack) BundleContentsOf() ([]ItemStack, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentBundleContents {
			items, ok := bundleItemsOf(raw.Data)
			if !ok {
				return nil, false
			}
			return items, true
		}
	}
	return nil, false
}

// BundleContents returns the items a bundle stack holds, decoding the
// minecraft:bundle_contents component it keeps as bytes. A bundle with no
// component holds nothing: the component is absent when it is the item's own
// default.
func (s ItemStack) BundleContents() []ItemStack {
	items, _ := s.BundleContentsOf()
	return items
}

// SetBundleContents replaces the items a bundle stack holds, storing them as
// the minecraft:bundle_contents component; an empty list removes the
// component, the absence the item's own default is stored as.
func (s *ItemStack) SetBundleContents(items []ItemStack) {
	if len(items) == 0 {
		s.dropRaw(DataComponentBundleContents)
		return
	}
	s.SetExplicitBundleContents(items)
}

// SetExplicitBundleContents writes minecraft:bundle_contents onto the stack,
// retaining an empty list as an explicit component patch.
func (s *ItemStack) SetExplicitBundleContents(items []ItemStack) {
	var value Buffer
	value.PutBundleContents(items)
	// A copied stack - the clone a creative middle-click makes - shares the
	// Raw backing array; the write below must not reach the stack it came
	// from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBundleContents, Data: value.Bytes()})
}

// bundleItemsOf decodes a minecraft:bundle_contents value: the items a bundle
// holds.
func bundleItemsOf(data []byte) ([]ItemStack, bool) {
	return readBundleContents(NewReader(data), 0)
}

// PutBundleContents writes a minecraft:bundle_contents value: the items a
// bundle holds, each the template a slot inside a component travels as — the
// item, the count and the component patch.
// vanilla: BundleContents.STREAM_CODEC
func (b *Buffer) PutBundleContents(items []ItemStack) {
	b.PutVarInt(int32(len(items)))
	for _, item := range items {
		b.PutItemTemplate(item)
	}
}

// PutItemTemplate writes an item's ID, count and component patch.
// vanilla: ItemStackTemplate.STREAM_CODEC
func (b *Buffer) PutItemTemplate(item ItemStack) {
	b.PutVarInt(item.ItemID)
	b.PutVarInt(item.Count)
	components := componentsOf(item)
	item.orderPatch(components)
	b.PutVarInt(int32(len(components)))
	b.PutVarInt(item.Removed.count())
	for _, component := range components {
		b.PutVarInt(component)
		b.PutComponentData(item, component)
	}
	b.putRemovedComponents(item)
}
