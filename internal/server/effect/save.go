package effect

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Encode is the entity file's active_effects list: defaults left out,
// show_icon always written; nil for no effects, where vanilla writes no list.
// vanilla: MobEffectInstance.CODEC
func Encode(all []Instance) *nbt.List {
	if len(all) == 0 {
		return nil
	}
	list := &nbt.List{Elem: nbt.TypeCompound}
	for i := range all {
		tag := nbt.NewOrderedCompound(7)
		tag.Set("id", nbt.String(registry.MobEffectName(all[i].ID)))
		encodeDetails(tag, &all[i])
		list.Items = append(list.Items, tag)
	}
	return list
}

// vanilla: MobEffectInstance.Details.MAP_CODEC
func encodeDetails(tag nbt.CompoundTag, in *Instance) {
	if in.Amplifier != 0 {
		tag.Set("amplifier", nbt.Byte(int8(in.Amplifier)))
	}
	if in.Duration != 0 {
		tag.Set("duration", nbt.Int(in.Duration))
	}
	if in.Ambient {
		tag.Set("ambient", boolTag(true))
	}
	if !in.Visible {
		tag.Set("show_particles", boolTag(false))
	}
	tag.Set("show_icon", boolTag(in.ShowIcon))
	if in.Hidden != nil {
		hidden := nbt.NewOrderedCompound(6)
		encodeDetails(hidden, in.Hidden)
		tag.Set("hidden_effect", hidden)
	}
}

// Decode reads an active_effects list into a fresh set, skipping entries
// that name no effect.
func Decode(list nbt.List) Set {
	var s Set
	for _, item := range list.Items {
		tag, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		name, _ := nbt.Get[nbt.String](tag, "id")
		id, ok := registry.MobEffectID(registry.Qualify(string(name)))
		if !ok {
			continue
		}
		in := decodeDetails(id, tag)
		s.active[id] = in
		s.mask |= uint64(1) << id
	}
	return s
}

func decodeDetails(id int32, tag nbt.Compound) *Instance {
	visible := boolOr(tag, "show_particles", true)
	in := New(id, int32(numberOr(tag, "duration", 0)), int32(uint8(numberOr(tag, "amplifier", 0))),
		boolOr(tag, "ambient", false), visible, boolOr(tag, "show_icon", visible))
	if hidden, ok := nbt.Get[nbt.Compound](tag, "hidden_effect"); ok {
		in.Hidden = decodeDetails(id, hidden)
	}
	return &in
}

func boolTag(v bool) nbt.Byte {
	if v {
		return 1
	}
	return 0
}

func boolOr(tag nbt.Compound, name string, def bool) bool {
	if _, ok := tag[name]; !ok {
		return def
	}
	return numberOr(tag, name, 0) != 0
}

// numberOr reads a field of any numeric tag type.
// vanilla: the numeric codecs over NbtOps
func numberOr(tag nbt.Compound, name string, def int64) int64 {
	switch v := tag[name].(type) {
	case nbt.Byte:
		return int64(v)
	case nbt.Short:
		return int64(v)
	case nbt.Int:
		return int64(v)
	case nbt.Long:
		return int64(v)
	case nbt.Float:
		return int64(v)
	case nbt.Double:
		return int64(v)
	}
	return def
}
