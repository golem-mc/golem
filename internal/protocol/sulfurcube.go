package protocol

import "slices"

// SetSulfurCubeContent saves the absorbed block as an item template.
// vanilla: SulfurCubeContent.STREAM_CODEC
func (s *ItemStack) SetSulfurCubeContent(item ItemStack) {
	if item.Empty() {
		return
	}
	var b Buffer
	b.PutItemTemplate(item)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentSulfurCubeContent, Data: b.Bytes()})
}

// SulfurCubeContent reads the absorbed block carried by this bucket.
func (s ItemStack) SulfurCubeContent() (ItemStack, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentSulfurCubeContent {
			r := NewReader(raw.Data)
			item, ok := readItemTemplate(r, 0)
			return item, ok && r.Remaining() == 0
		}
	}
	return ItemStack{}, false
}

// ReadItemTemplate reads an item's ID, count and component patch.
func (r *Reader) ReadItemTemplate() (ItemStack, bool) {
	return readItemTemplate(r, 0)
}

// vanilla: ItemStackTemplate.STREAM_CODEC
func readItemTemplate(r *Reader, depth int) (ItemStack, bool) {
	item, count := r.ReadVarInt(), r.ReadVarInt()
	if r.Err() != nil || item <= 0 || item >= RegistrySizeItem || count < 1 {
		return ItemStack{}, false
	}
	return readSlotPatch(r, ItemStack{ItemID: item, Count: count}, depth+1)
}
