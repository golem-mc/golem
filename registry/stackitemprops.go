package registry

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// propComponent is a component that overrides an item's prototype, held as
// its wire bytes: read turns the codec's NBT into them, show the reverse (false
// for a value with no NBT form, such as an inline holder).
type propComponent struct {
	name string
	typ  int32
	read func(value nbt.Tag, b *protocol.Buffer) bool
	show func(r *protocol.Reader) (nbt.Tag, bool)
}

var propComponents = [...]propComponent{
	// vanilla: DataComponents.MAX_DAMAGE, ExtraCodecs.POSITIVE_INT
	{"minecraft:max_damage", protocol.DataComponentMaxDamage, readPositive(""), showPositive("")},
	// vanilla: Enchantable.CODEC
	{"minecraft:enchantable", protocol.DataComponentEnchantable, readPositive("value"), showPositive("value")},
	// vanilla: DataComponents.MINIMUM_ATTACK_CHARGE, ExtraCodecs.floatRange(0, 1)
	{"minecraft:minimum_attack_charge", protocol.DataComponentMinimumAttackCharge,
		func(value nbt.Tag, b *protocol.Buffer) bool {
			f, ok := nbt.NumberValue(value)
			if !ok || !(f >= 0 && f <= 1) { // not NaN either
				return false
			}
			b.PutF32(float32(f))
			return true
		},
		func(r *protocol.Reader) (nbt.Tag, bool) { return nbt.Float(r.ReadF32()), r.Err() == nil }},
	// vanilla: DamageType.CODEC
	{"minecraft:damage_type", protocol.DataComponentDamageType, readRef("minecraft:damage_type", 0), showRef("minecraft:damage_type", 0)},
	// vanilla: DecoratedPotPatterns.CODEC
	{"minecraft:provides_pottery_pattern", protocol.DataComponentProvidesPotteryPattern,
		readRef("minecraft:decorated_pot_pattern", 0), showRef("minecraft:decorated_pot_pattern", 0)},
	// An inline material is neither read from a file nor written to one.
	// vanilla: TrimMaterial.CODEC
	{"minecraft:provides_trim_material", protocol.DataComponentProvidesTrimMaterial,
		readRef("minecraft:trim_material", 1), showRef("minecraft:trim_material", 1)},
	// vanilla: Repairable.CODEC
	{"minecraft:repairable", protocol.DataComponentRepairable,
		readItemsField(readSet("minecraft:item")), showItemsField(showSet("minecraft:item"))},
	// vanilla: RegistryCodecs.holderSet(BANNER_PATTERN)
	{"minecraft:provides_banner_patterns", protocol.DataComponentProvidesBannerPatterns,
		readSet("minecraft:banner_pattern"), showSet("minecraft:banner_pattern")},
}

// propDefaults reports, for the propComponents the item has its own value
// of, whether the wire bytes equal that value.
var propDefaults = map[int32]func(item int32, b []byte) bool{
	protocol.DataComponentMaxDamage:   varIntDefault(MaxDamage),
	protocol.DataComponentEnchantable: varIntDefault(Enchantable),
	protocol.DataComponentMinimumAttackCharge: func(item int32, b []byte) bool {
		r := protocol.NewReader(b)
		f := r.ReadF32()
		return r.Err() == nil && f == MinimumAttackCharge(item)
	},
	protocol.DataComponentDamageType: func(item int32, b []byte) bool {
		r := protocol.NewReader(b)
		id := r.ReadVarInt()
		d := ItemDamageType(item)
		return r.Err() == nil && d > 0 && id == int32(d)-1
	},
}

func varIntDefault(own func(item int32) int32) func(int32, []byte) bool {
	return func(item int32, b []byte) bool {
		r := protocol.NewReader(b)
		n := r.ReadVarInt()
		return r.Err() == nil && n == own(item)
	}
}

func readPositive(field string) func(nbt.Tag, *protocol.Buffer) bool {
	return func(value nbt.Tag, b *protocol.Buffer) bool {
		if field != "" {
			entry, ok := nbt.CompoundView(value)
			if !ok {
				return false
			}
			value = entry[field]
		}
		n, ok := nbt.IntValue(value)
		if ok && n >= 1 {
			b.PutVarInt(n)
		}
		return ok && n >= 1
	}
}

func showPositive(field string) func(*protocol.Reader) (nbt.Tag, bool) {
	return func(r *protocol.Reader) (nbt.Tag, bool) {
		n := r.ReadVarInt()
		if field == "" {
			return nbt.Int(n), r.Err() == nil
		}
		tag := nbt.NewOrderedCompound(1)
		tag.Set(field, nbt.Int(n))
		return tag, r.Err() == nil
	}
}

// readRef reads a registry entry name; shift is what the wire adds to its ID
// (1 where zero is an inline holder).
func readRef(registry string, shift int32) func(nbt.Tag, *protocol.Buffer) bool {
	return func(value nbt.Tag, b *protocol.Buffer) bool {
		id, ok := entryID(registry, value)
		if ok {
			b.PutVarInt(id + shift)
		}
		return ok
	}
}

func showRef(registry string, shift int32) func(*protocol.Reader) (nbt.Tag, bool) {
	return func(r *protocol.Reader) (nbt.Tag, bool) {
		id := r.ReadVarInt() - shift
		name, ok := entryName(registry, id)
		return nbt.String(name), ok && r.Err() == nil
	}
}

// tagExists reports whether the registry has a tag of this name.
func tagExists(registry, tag string) bool {
	if registry == registryItem {
		return ItemTagOf(tag) != noItemTag
	}
	_, ok := SyncTagMembers(registry)(tag)
	return ok
}

// readSet reads a holder set: "#tag", an entry name or a list of names.
// vanilla: HolderSetCodec
func readSet(registry string) func(nbt.Tag, *protocol.Buffer) bool {
	return func(value nbt.Tag, b *protocol.Buffer) bool {
		names := []nbt.Tag{value}
		if list, ok := value.(nbt.List); ok {
			names = list.Items
		}
		if s, ok := value.(nbt.String); ok && strings.HasPrefix(string(s), "#") {
			tag := Qualify(string(s[1:]))
			if !protocol.ValidIdentifier(tag) || !tagExists(registry, tag) {
				return false
			}
			b.PutVarInt(0)
			b.PutString(tag)
			return true
		}
		ids := make([]int32, len(names))
		for i, n := range names {
			id, ok := entryID(registry, n)
			if !ok {
				return false
			}
			ids[i] = id
		}
		b.PutVarInt(int32(len(ids)) + 1)
		for _, id := range ids {
			b.PutVarInt(id)
		}
		return true
	}
}

// showSet writes one entry as a bare name, any other count as a list.
func showSet(registry string) func(*protocol.Reader) (nbt.Tag, bool) {
	return func(r *protocol.Reader) (nbt.Tag, bool) {
		n := r.ReadVarInt()
		if n == 0 {
			return nbt.String("#" + r.ReadString(32767)), r.Err() == nil
		}
		names := make([]nbt.Tag, 0, n-1)
		for range n - 1 {
			name, ok := entryName(registry, r.ReadVarInt())
			if !ok || r.Err() != nil {
				return nil, false
			}
			names = append(names, nbt.String(name))
		}
		if len(names) == 1 {
			return names[0], true
		}
		return nbt.List{Items: names}, true
	}
}

func readItemsField(inner func(nbt.Tag, *protocol.Buffer) bool) func(nbt.Tag, *protocol.Buffer) bool {
	return func(value nbt.Tag, b *protocol.Buffer) bool {
		entry, ok := nbt.CompoundView(value)
		return ok && inner(entry["items"], b)
	}
}

func showItemsField(inner func(*protocol.Reader) (nbt.Tag, bool)) func(*protocol.Reader) (nbt.Tag, bool) {
	return func(r *protocol.Reader) (nbt.Tag, bool) {
		items, ok := inner(r)
		tag := nbt.NewOrderedCompound(1)
		tag.Set("items", items)
		return tag, ok
	}
}

// propBytes is the stack's own wire bytes of a component.
func propBytes(stack protocol.ItemStack, typ int32) ([]byte, bool) {
	for _, raw := range stack.Raw {
		if raw.Type == typ {
			return raw.Data, true
		}
	}
	return nil, false
}

// readPropComponent is readComponentKnown for the propComponents.
func readPropComponent(stack *protocol.ItemStack, name string, value nbt.Tag) (ok, modeled bool) {
	for i := range propComponents {
		if c := &propComponents[i]; c.name == name {
			var b protocol.Buffer
			if !c.read(value, &b) {
				return false, true
			}
			if isDefault := propDefaults[c.typ]; isDefault != nil && isDefault(stack.ItemID, b.Bytes()) {
				stack.ResetComponent(c.typ)
				return true, true
			}
			stack.SetRaw(protocol.RawComponent{Type: c.typ, Data: b.Bytes()})
			return true, true
		}
	}
	return false, false
}

func withPropTags(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	for i := range propComponents {
		c := &propComponents[i]
		data, ok := propBytes(stack, c.typ)
		if !ok {
			continue
		}
		if tag, ok := c.show(protocol.NewReader(data)); ok {
			if components == nil {
				components = nbt.Compound{}
			}
			components[c.name] = tag
		}
	}
	return components
}

// stackVarInt is the VarInt a component's bytes hold; false when the stack has
// no such component of its own.
func stackVarInt(stack protocol.ItemStack, typ int32) (int32, bool) {
	data, ok := propBytes(stack, typ)
	if !ok {
		return 0, false
	}
	r := protocol.NewReader(data)
	n := r.ReadVarInt()
	return n, r.Err() == nil
}

// StackMaxDamage returns the wear the stack takes before it breaks: its own
// max_damage, 0 when the patch removes it, else the item's.
// vanilla: ItemStack.getMaxDamage
func StackMaxDamage(stack protocol.ItemStack) int32 {
	if n, ok := stackVarInt(stack, protocol.DataComponentMaxDamage); ok {
		return n
	}
	if stack.Removes(protocol.DataComponentMaxDamage) {
		return 0
	}
	return MaxDamage(stack.ItemID)
}

// SetStackMaxDamage writes a positive maximum, dropping a patch equal to the item default.
func SetStackMaxDamage(stack *protocol.ItemStack, maxDamage int32) {
	if maxDamage == MaxDamage(stack.ItemID) {
		stack.ResetComponent(protocol.DataComponentMaxDamage)
		return
	}
	if current, ok := stackVarInt(*stack, protocol.DataComponentMaxDamage); ok && current == maxDamage {
		return
	}
	var data protocol.Buffer
	data.PutVarInt(maxDamage)
	stack.Raw = slices.Clone(stack.Raw)
	stack.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: data.Bytes()})
}

// StackDamageable reports whether the stack can accumulate wear.
// vanilla: ItemStack.isDamageableItem
func StackDamageable(stack protocol.ItemStack) bool {
	return !stack.Unbreakable && !stack.Removes(protocol.DataComponentDamage) && StackMaxDamage(stack) > 0
}

// StackEnchantable returns the stack's enchantability: its own, 0 when the
// patch removes it, else the item's.
// vanilla: ItemStack.getEnchantmentValue
func StackEnchantable(stack protocol.ItemStack) int32 {
	if n, ok := stackVarInt(stack, protocol.DataComponentEnchantable); ok {
		return n
	}
	if stack.Removes(protocol.DataComponentEnchantable) {
		return 0
	}
	return Enchantable(stack.ItemID)
}

// StackRepairs reports whether the stack's repairable holder set takes other:
// its own, none when the patch removes it, else the item's.
// vanilla: Repairable.isValidRepairItem
func StackRepairs(stack protocol.ItemStack, other int32) bool {
	data, ok := propBytes(stack, protocol.DataComponentRepairable)
	if !ok {
		if stack.Removes(protocol.DataComponentRepairable) {
			return false
		}
		return RepairsWith(stack.ItemID, other)
	}
	r := protocol.NewReader(data)
	n := r.ReadVarInt()
	if n == 0 {
		return Item(other).InTag(ItemTagOf(r.ReadString(32767)))
	}
	for range n - 1 {
		if r.ReadVarInt() == other {
			return true
		}
	}
	return false
}

// registryItem names the item registry, which is built in rather than synchronized.
const registryItem = "minecraft:item"
