package protocol

import (
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// SetCustomData lays minecraft:custom_data on the stack; an empty compound
// removes it, the item's own default.
// vanilla: CustomData.set
func (s *ItemStack) SetCustomData(tag nbt.Compound) {
	s.SetCustomDataTag(tag)
}

// SetCustomDataTag sets custom data while retaining compound insertion history.
func (s *ItemStack) SetCustomDataTag(tag nbt.CompoundTag) {
	if tag == nil || len(tag.Entries()) == 0 {
		s.dropRaw(DataComponentCustomData)
		return
	}
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		return
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentCustomData, Data: data})
}

// CustomData is the stack's minecraft:custom_data, nil when it carries none
// or one that does not read. Nested compounds are plain maps.
func (s ItemStack) CustomData() nbt.Compound {
	if tag := s.CustomDataTag(); tag != nil {
		return nbt.PlainCopy(tag).(nbt.Compound)
	}
	return nil
}

// CustomDataTag reads custom data with its compound insertion history.
func (s ItemStack) CustomDataTag() nbt.CompoundTag {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentCustomData {
			continue
		}
		tag, _, err := nbt.DecodeNetworkOrdered(raw.Data)
		if c, ok := tag.(nbt.CompoundTag); ok && err == nil {
			return c
		}
	}
	return nil
}
