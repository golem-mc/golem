package registry

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// customModelDataTag writes custom_model_data with only the lists that have
// entries: an empty list equals the field's default, which the codec leaves
// out.
// vanilla: CustomModelData.CODEC (optionalFieldOf with List.of())
func customModelDataTag(d protocol.CustomModelData) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(0)
	list := func(key string, elem nbt.Type, n int, at func(int) nbt.Tag) {
		if n == 0 {
			return
		}
		items := make([]nbt.Tag, n)
		for i := range items {
			items[i] = at(i)
		}
		tag.Set(key, nbt.List{Elem: elem, Items: items})
	}
	list("floats", nbt.TypeFloat, len(d.Floats), func(i int) nbt.Tag { return nbt.Float(d.Floats[i]) })
	list("flags", nbt.TypeByte, len(d.Flags), func(i int) nbt.Tag {
		if d.Flags[i] {
			return nbt.Byte(1)
		}
		return nbt.Byte(0)
	})
	list("strings", nbt.TypeString, len(d.Strings), func(i int) nbt.Tag { return nbt.String(d.Strings[i]) })
	list("colors", nbt.TypeInt, len(d.Colors), func(i int) nbt.Tag { return nbt.Int(d.Colors[i]) })
	return tag
}

// customModelDataOf reads custom_model_data; each list is optional and a
// colour is an int or a list of three channel floats.
// vanilla: CustomModelData.CODEC, ExtraCodecs.RGB_COLOR_CODEC
func customModelDataOf(value nbt.Tag) (protocol.CustomModelData, bool) {
	tag, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.CustomModelData{}, false
	}
	var d protocol.CustomModelData
	each := func(key string, read func(nbt.Tag) bool) bool {
		v, present := tag[key]
		if !present {
			return true
		}
		items := nbt.CollectionValues(v)
		if items == nil {
			return false
		}
		for item := range items {
			if !read(item) {
				return false
			}
		}
		return true
	}
	ok = each("floats", func(t nbt.Tag) bool {
		f, ok := floatOf(t)
		d.Floats = append(d.Floats, f)
		return ok
	}) && each("flags", func(t nbt.Tag) bool {
		b, ok := boolValue(t)
		d.Flags = append(d.Flags, b)
		return ok
	}) && each("strings", func(t nbt.Tag) bool {
		s, ok := t.(nbt.String)
		d.Strings = append(d.Strings, string(s))
		return ok
	}) && each("colors", func(t nbt.Tag) bool {
		c, ok := RGBColorOf(t)
		d.Colors = append(d.Colors, c)
		return ok
	})
	return d, ok
}

// RGBColorOf reads an RGB colour: an int, or a list of three channel floats
// from 0 to 1.
// vanilla: ExtraCodecs.RGB_COLOR_CODEC, ARGB.colorFromFloat
func RGBColorOf(value nbt.Tag) (int32, bool) {
	if c, ok := nbt.IntValue(value); ok {
		return c, true
	}
	items := nbt.CollectionValues(value)
	if items == nil {
		return 0, false
	}
	rgb := int32(-1 << 24) // opaque: colorFromFloat's alpha of 1
	i := 0
	for item := range items {
		f, ok := floatOf(item)
		if !ok || i == 3 {
			return 0, false
		}
		rgb |= int32(math.Floor(float64(f*255))) & 0xFF << (16 - 8*i) // vanilla: ARGB.as8BitChannel
		i++
	}
	if i != 3 {
		return 0, false
	}
	return rgb, true
}

// containerLootTag writes container_loot, the seed only when set.
// vanilla: SeededContainerLoot.CODEC
func containerLootTag(loot protocol.ContainerLoot) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(1)
	tag.Set("loot_table", nbt.String(loot.Table))
	if loot.Seed != 0 {
		tag.Set("seed", nbt.Long(loot.Seed))
	}
	return tag
}

// containerLootOf reads container_loot: a table name and an optional seed.
func containerLootOf(value nbt.Tag) (protocol.ContainerLoot, bool) {
	tag, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.ContainerLoot{}, false
	}
	name, ok := tag["loot_table"].(nbt.String)
	if !ok || name == "" {
		return protocol.ContainerLoot{}, false
	}
	loot := protocol.ContainerLoot{Table: Qualify(string(name))}
	if seed, present := tag["seed"]; present {
		if loot.Seed, ok = nbt.LongValue(seed); !ok {
			return protocol.ContainerLoot{}, false
		}
	}
	return loot, true
}

// readModelDataComponent is readComponentKnown for custom_model_data and
// container_loot.
func readModelDataComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:custom_model_data":
		d, ok := customModelDataOf(value)
		if !ok {
			return false, true
		}
		stack.SetCustomModelData(d)
	case "minecraft:container_loot":
		loot, ok := containerLootOf(value)
		if !ok {
			return false, true
		}
		stack.ContainerLoot = loot
	default:
		if ok, modeled := readAdventureOrEntityData(stack, name, value); modeled {
			return ok, modeled
		}
		return readDisplayComponent(stack, name, value)
	}
	return true, true
}
