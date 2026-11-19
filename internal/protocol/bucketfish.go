package protocol

import (
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// SetBucketEntityData lays minecraft:bucket_entity_data on the stack: the
// compound a mob bucket spawns its mob from.
// vanilla: Bucketable.saveDefaultDataToBucketTag
func (s *ItemStack) SetBucketEntityData(tag nbt.CompoundTag) {
	if tag == nil {
		tag = nbt.Compound{}
	}
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		return
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBucketEntityData, Data: data})
}

// BucketEntityData is the stack's minecraft:bucket_entity_data, nil when it
// carries none or one that does not read.
func (s ItemStack) BucketEntityData() nbt.Compound {
	tag, _ := nbt.CompoundView(s.BucketEntityDataTag())
	return tag
}

// BucketEntityDataTag reads bucket data retaining compound insertion history.
func (s ItemStack) BucketEntityDataTag() nbt.CompoundTag {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBucketEntityData {
			continue
		}
		tag, _, err := nbt.DecodeNetworkOrdered(raw.Data)
		if c, ok := tag.(nbt.CompoundTag); ok && err == nil {
			return c
		}
	}
	return nil
}

// EnumOf is an enum component's wire id (minecraft:salmon/size,
// minecraft:rabbit/variant and the like) and whether the stack carries one.
func (s ItemStack) EnumOf(component int32) (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			r := NewReader(raw.Data)
			id := r.ReadVarInt()
			return id, r.Err() == nil
		}
	}
	return 0, false
}

// SetEnum lays an enum component on the stack by its wire id.
// vanilla: ByteBufCodecs.idMapper
func (s *ItemStack) SetEnum(component, id int32) {
	var value Buffer
	value.PutVarInt(id)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: component, Data: value.Bytes()})
}

// SetSalmonSize lays minecraft:salmon/size on the stack, a Salmon.Variant id.
func (s *ItemStack) SetSalmonSize(size int32) { s.SetEnum(DataComponentSalmonSize, size) }

// SalmonSize is the stack's minecraft:salmon/size and whether it carries one.
func (s ItemStack) SalmonSize() (int32, bool) { return s.EnumOf(DataComponentSalmonSize) }

// SetAxolotlVariant lays minecraft:axolotl/variant on the bucket.
// vanilla: Axolotl.saveToBucketTag, Variant.STREAM_CODEC
func (s *ItemStack) SetAxolotlVariant(variant int32) { s.SetEnum(DataComponentAxolotlVariant, variant) }

// AxolotlVariant is the bucket's minecraft:axolotl/variant component.
func (s ItemStack) AxolotlVariant() (int32, bool) { return s.EnumOf(DataComponentAxolotlVariant) }

// SetTropicalFishVariant writes the packed variant as three bucket components.
// vanilla: TropicalFish.saveToBucketTag, Pattern.STREAM_CODEC, DyeColor.STREAM_CODEC
func (s *ItemStack) SetTropicalFishVariant(variant int32) {
	s.Raw = slices.Clone(s.Raw)
	for _, part := range []struct{ typ, value int32 }{
		{DataComponentTropicalFishPattern, variant & 0xffff},
		{DataComponentTropicalFishBaseColor, variant >> 16 & 0xff},
		{DataComponentTropicalFishPatternColor, variant >> 24 & 0xff},
	} {
		var value Buffer
		value.PutVarInt(part.value)
		s.putRaw(RawComponent{Type: part.typ, Data: value.Bytes()})
	}
}

// TropicalFishVariant applies the bucket's variant components to fallback.
func (s ItemStack) TropicalFishVariant(fallback int32) int32 {
	for _, raw := range s.Raw {
		var mask, shift int32
		switch raw.Type {
		case DataComponentTropicalFishPattern:
			mask = 0xffff
		case DataComponentTropicalFishBaseColor:
			mask, shift = 0xff, 16
		case DataComponentTropicalFishPatternColor:
			mask, shift = 0xff, 24
		default:
			continue
		}
		r := NewReader(raw.Data)
		value := r.ReadVarInt()
		if r.Err() == nil {
			// The stream codecs map unknown ids to kob or white.
			if (shift == 0 && (value < 0 || value&0xff > 1 || value>>8 > 5)) || (shift != 0 && (value < 0 || value > 15)) {
				value = 0
			}
			fallback = fallback & ^(mask<<shift) | (value&mask)<<shift
		}
	}
	return fallback
}
