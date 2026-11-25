package registry

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// enumValue is one constant of an enum component: its wire id and the name
// its codec writes.
type enumValue struct {
	id   int32
	name string
}

// enumComponent is a component whose codec writes an enum by its lower-case
// name and whose stream codec sends the id. values[0] is what an unknown id
// decodes to, unless oob is clamp (an id past either end is that end) or wrap
// (floorMod by the count).
// vanilla: StringRepresentable.fromEnum, ByIdMap
type enumComponent struct {
	name   string
	typ    int32
	values []enumValue
	oob    oobStrategy
}

// oobStrategy is a ByIdMap.OutOfBoundsStrategy.
type oobStrategy uint8

const (
	zero oobStrategy = iota
	clamp
	wrap
)

var enumComponents = [...]enumComponent{
	// vanilla: Axolotl.Variant (ByIdMap ZERO)
	{"minecraft:axolotl/variant", protocol.DataComponentAxolotlVariant, []enumValue{
		{0, "lucy"}, {1, "wild"}, {2, "gold"}, {3, "cyan"}, {4, "blue"},
	}, zero},
	// vanilla: Salmon.Variant (ByIdMap CLAMP)
	{"minecraft:salmon/size", protocol.DataComponentSalmonSize, []enumValue{
		{0, "small"}, {1, "medium"}, {2, "large"},
	}, clamp},
	// vanilla: Rabbit.Variant (ByIdMap.sparse, DEFAULT brown)
	{"minecraft:rabbit/variant", protocol.DataComponentRabbitVariant, []enumValue{
		{0, "brown"}, {1, "white"}, {2, "black"}, {3, "white_splotched"}, {4, "gold"}, {5, "salt"}, {99, "evil"},
	}, zero},
	// The id is the packed one, base size | index << 8.
	// vanilla: TropicalFish.Pattern (ByIdMap.sparse, KOB)
	{"minecraft:tropical_fish/pattern", protocol.DataComponentTropicalFishPattern, []enumValue{
		{0, "kob"}, {1 << 8, "sunstreak"}, {2 << 8, "snooper"}, {3 << 8, "dasher"}, {4 << 8, "brinely"}, {5 << 8, "spotty"},
		{1, "flopper"}, {1 | 1<<8, "stripey"}, {1 | 2<<8, "glitter"}, {1 | 3<<8, "blockfish"}, {1 | 4<<8, "betty"}, {1 | 5<<8, "clayfish"},
	}, zero},
	// vanilla: Fox.Variant (ByIdMap ZERO)
	{"minecraft:fox/variant", protocol.DataComponentFoxVariant, []enumValue{{0, "red"}, {1, "snow"}}, zero},
	// vanilla: Parrot.Variant (ByIdMap CLAMP)
	{"minecraft:parrot/variant", protocol.DataComponentParrotVariant, []enumValue{
		{0, "red_blue"}, {1, "blue"}, {2, "green"}, {3, "yellow_blue"}, {4, "gray"},
	}, clamp},
	// vanilla: MushroomCow.Variant (ByIdMap CLAMP)
	{"minecraft:mooshroom/variant", protocol.DataComponentMooshroomVariant, []enumValue{{0, "red"}, {1, "brown"}}, clamp},
	// vanilla: equine.Variant (ByIdMap WRAP)
	{"minecraft:horse/variant", protocol.DataComponentHorseVariant, []enumValue{
		{0, "white"}, {1, "creamy"}, {2, "chestnut"}, {3, "brown"}, {4, "black"}, {5, "gray"}, {6, "dark_brown"},
	}, wrap},
	// vanilla: Llama.Variant (ByIdMap CLAMP)
	{"minecraft:llama/variant", protocol.DataComponentLlamaVariant, []enumValue{
		{0, "creamy"}, {1, "white"}, {2, "brown"}, {3, "gray"},
	}, clamp},
	// vanilla: DyeColor (ByIdMap ZERO)
	{"minecraft:dye", protocol.DataComponentDye, dyeEnumValues(), zero},
	{"minecraft:wolf/collar", protocol.DataComponentWolfCollar, dyeEnumValues(), zero},
	{"minecraft:cat/collar", protocol.DataComponentCatCollar, dyeEnumValues(), zero},
	{"minecraft:sheep/color", protocol.DataComponentSheepColor, dyeEnumValues(), zero},
	{"minecraft:shulker/color", protocol.DataComponentShulkerColor, dyeEnumValues(), zero},
	{"minecraft:cushion/color", protocol.DataComponentCushionColor, dyeEnumValues(), zero},
	{"minecraft:tropical_fish/base_color", protocol.DataComponentTropicalFishBaseColor, dyeEnumValues(), zero},
	{"minecraft:tropical_fish/pattern_color", protocol.DataComponentTropicalFishPatternColor, dyeEnumValues(), zero},
}

// dyeEnumValues lists DyeNames by their DyeColor ids.
func dyeEnumValues() []enumValue {
	values := make([]enumValue, len(DyeNames))
	for i, name := range DyeNames {
		values[i] = enumValue{int32(i), name}
	}
	return values
}

// decode is the constant the stream codec reads id as.
func (c *enumComponent) decode(id int32) enumValue {
	for _, v := range c.values {
		if v.id == id {
			return v
		}
	}
	switch {
	case c.oob == clamp && id > 0:
		return c.values[len(c.values)-1]
	case c.oob == wrap:
		return c.values[(int(id)%len(c.values)+len(c.values))%len(c.values)]
	}
	return c.values[0]
}

// holderComponents are the components naming one registry entry. A stack
// whose holder is inline (wire ID 0) is not written, and an inline value
// read from a file is refused.
// vanilla: RegistryCodecs.holder
var holderComponents = [...]struct {
	name, registry string
	of             func(protocol.ItemStack) (int32, bool)
	set            func(*protocol.ItemStack, int32)
}{
	// vanilla: InstrumentComponent.CODEC
	{"minecraft:instrument", "minecraft:instrument", protocol.ItemStack.InstrumentOf, (*protocol.ItemStack).SetInstrument},
	// vanilla: PaintingVariant.CODEC
	{"minecraft:painting/variant", "minecraft:painting_variant", protocol.ItemStack.PaintingVariantOf, (*protocol.ItemStack).SetPaintingVariant},
}

// rawHolderComponents are the components naming one entry of a registry by a
// bare registry ID, with no inline form. builtin lists the entries of a
// registry the client has built in and the server does not synchronize.
// vanilla: ByteBufCodecs.holderRegistry
var rawHolderComponents = [...]struct {
	name, registry string
	typ            int32
	builtin        []string
}{
	{"minecraft:villager/variant", "minecraft:villager_type", protocol.DataComponentVillagerVariant, []string{
		"minecraft:desert", "minecraft:jungle", "minecraft:plains", "minecraft:savanna",
		"minecraft:snow", "minecraft:swamp", "minecraft:taiga",
	}},
	{"minecraft:wolf/variant", "minecraft:wolf_variant", protocol.DataComponentWolfVariant, nil},
	{"minecraft:wolf/sound_variant", "minecraft:wolf_sound_variant", protocol.DataComponentWolfSoundVariant, nil},
	{"minecraft:pig/variant", "minecraft:pig_variant", protocol.DataComponentPigVariant, nil},
	{"minecraft:pig/sound_variant", "minecraft:pig_sound_variant", protocol.DataComponentPigSoundVariant, nil},
	{"minecraft:cow/variant", "minecraft:cow_variant", protocol.DataComponentCowVariant, nil},
	{"minecraft:cow/sound_variant", "minecraft:cow_sound_variant", protocol.DataComponentCowSoundVariant, nil},
	{"minecraft:chicken/variant", "minecraft:chicken_variant", protocol.DataComponentChickenVariant, nil},
	{"minecraft:chicken/sound_variant", "minecraft:chicken_sound_variant", protocol.DataComponentChickenSoundVariant, nil},
	{"minecraft:zombie_nautilus/variant", "minecraft:zombie_nautilus_variant", protocol.DataComponentZombieNautilusVariant, nil},
	{"minecraft:frog/variant", "minecraft:frog_variant", protocol.DataComponentFrogVariant, nil},
	{"minecraft:cat/variant", "minecraft:cat_variant", protocol.DataComponentCatVariant, nil},
	{"minecraft:cat/sound_variant", "minecraft:cat_sound_variant", protocol.DataComponentCatSoundVariant, nil},
}

// putVariantComponents adds the stack's trim, holder, enum and
// potion_duration_scale components to components, allocating it when nil,
// and returns it.
func putVariantComponents(stack protocol.ItemStack, components nbt.Compound) nbt.Compound {
	put := func(name string, tag nbt.Tag) {
		if components == nil {
			components = nbt.Compound{}
		}
		components[name] = tag
	}
	if material, pattern, ok := stack.TrimOf(); ok {
		m, mok := entryName("minecraft:trim_material", material)
		p, pok := entryName("minecraft:trim_pattern", pattern)
		if mok && pok {
			// vanilla: ArmorTrim.CODEC
			tag := nbt.NewOrderedCompound(2)
			tag.Set("material", nbt.String(m))
			tag.Set("pattern", nbt.String(p))
			put("minecraft:trim", tag)
		}
	}
	for _, h := range holderComponents {
		if id, ok := h.of(stack); ok {
			if name, ok := entryName(h.registry, id); ok {
				put(h.name, nbt.String(name))
			}
		}
	}
	for _, h := range rawHolderComponents {
		if id, ok := stack.EnumOf(h.typ); ok {
			if name, ok := entryNameOf(h.registry, h.builtin, id); ok {
				put(h.name, nbt.String(name))
			}
		}
	}
	for i := range enumComponents {
		c := &enumComponents[i]
		if id, ok := stack.EnumOf(c.typ); ok {
			put(c.name, nbt.String(c.decode(id).name))
		}
	}
	if scale, ok := stack.PotionDurationScale(); ok {
		put("minecraft:potion_duration_scale", nbt.Float(scale))
	}
	return components
}

// readVariantComponent is readComponentKnown for the components
// putVariantComponents writes.
func (lookup *TagLookup) readVariantComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	switch name {
	case "minecraft:block_transformer":
		return readBlockTransformer(stack, value), true
	case "minecraft:trim":
		tag, isMap := nbt.CompoundView(value)
		if !isMap {
			return false, true
		}
		material, mok := entryID("minecraft:trim_material", tag["material"])
		pattern, pok := entryID("minecraft:trim_pattern", tag["pattern"])
		if !mok || !pok {
			return false, true
		}
		stack.SetTrim(material, pattern)
		return true, true
	case "minecraft:potion_duration_scale":
		f, isNumber := nbt.NumberValue(value)
		scale := float32(f)
		// vanilla: ExtraCodecs.NON_NEGATIVE_FLOAT, read as Number.floatValue
		if !isNumber || !(scale >= 0) || math.IsInf(float64(scale), 1) {
			return false, true
		}
		stack.SetPotionDurationScale(scale)
		return true, true
	}
	for _, h := range holderComponents {
		if h.name == name {
			id, ok := entryID(h.registry, value)
			if ok {
				h.set(stack, id)
			}
			return ok, true
		}
	}
	for _, h := range rawHolderComponents {
		if h.name == name {
			id, ok := entryIDOf(h.registry, h.builtin, value)
			if ok {
				stack.SetEnum(h.typ, id)
			}
			return ok, true
		}
	}
	for i := range enumComponents {
		c := &enumComponents[i]
		if c.name != name {
			continue
		}
		s, _ := value.(nbt.String)
		for _, v := range c.values {
			if v.name == string(s) {
				stack.SetEnum(c.typ, v.id)
				return true, true
			}
		}
		return false, true
	}
	if ok, modeled := readPropComponent(stack, name, value); modeled {
		return ok, true
	}
	return lookup.readModelDataComponent(stack, name, value)
}

// entryName is the name of a synchronized registry's entry (or an item's), false for an ID
// past it.
func entryName(registry string, id int32) (string, bool) { return entryNameOf(registry, nil, id) }

// entryNameOf is entryName with builtin standing in for a registry that is
// not synchronized.
func entryNameOf(registry string, builtin []string, id int32) (string, bool) {
	if registry == registryItem {
		return ItemName(id)
	}
	r, ok := Get(registry)
	if !ok && builtin != nil {
		r, ok = Registry{Entries: builtin}, true
	}
	if !ok || id < 0 || int(id) >= len(r.Entries) {
		return "", false
	}
	return r.Entries[id], true
}

// entryID reads a registry reference: a string naming an entry of the
// registry, the namespace defaulting to minecraft. Anything else, an inline
// value among them, is refused.
func entryID(registry string, value nbt.Tag) (int32, bool) { return entryIDOf(registry, nil, value) }

// entryIDOf is entryID with builtin standing in for a registry that is not
// synchronized.
func entryIDOf(registry string, builtin []string, value nbt.Tag) (int32, bool) {
	name, ok := value.(nbt.String)
	if !ok {
		return 0, false
	}
	if registry == registryItem {
		return ItemID(Qualify(string(name)))
	}
	r, ok := Get(registry)
	if !ok && builtin != nil {
		r, ok = Registry{Entries: builtin}, true
	}
	if !ok {
		return 0, false
	}
	id := r.ID(Qualify(string(name)))
	return id, id >= 0
}

// StackVariant is the entry a mob-variant component of the stack names: an
// enum component's canonical wire id and serialized name, or a registry
// holder's id and entry name.
// vanilla: Entity.applyComponentsFromItemStack
func StackVariant(stack protocol.ItemStack, component int32) (id int32, name string, ok bool) {
	raw, found := stack.EnumOf(component)
	if !found {
		return 0, "", false
	}
	for i := range enumComponents {
		if c := &enumComponents[i]; c.typ == component {
			v := c.decode(raw)
			return v.id, v.name, true
		}
	}
	for _, h := range rawHolderComponents {
		if h.typ == component {
			name, ok = entryNameOf(h.registry, h.builtin, raw)
			return raw, name, ok
		}
	}
	return 0, "", false
}

// EnumComponentName is the name an enum component's codec writes for a wire
// id, false for a component that is not an enum.
func EnumComponentName(component string, id int32) (string, bool) {
	for i := range enumComponents {
		if enumComponents[i].name == component {
			return enumComponents[i].decode(id).name, true
		}
	}
	return "", false
}
