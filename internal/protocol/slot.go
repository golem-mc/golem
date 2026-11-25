package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math/bits"
	"reflect"
	"slices"

	"github.com/golem-mc/golem/nbt"
)

// ItemStack is one inventory slot: a count of one item with its data
// components. A Count of zero is an empty slot, whatever ItemID says.
//
// Eight components are modelled; any other the wire codecs read (a potion's
// contents) rides as the bytes it arrived in, checked against its codec, so
// the stack can be re-sent unchanged. See Raw and Reader.ReadUntrustedSlot.
type ItemStack struct {
	// ItemID is the ID in the minecraft:item registry, which is built into
	// the client and version-bound. registry.ItemName gives the name.
	ItemID int32
	Count  int32

	// Damage is minecraft:damage, zero for a fresh item. Items break at
	// max damage, so the wire never carries the wear that ends one.
	Damage int32

	// Enchantments is minecraft:enchantments, registry ID to level; empty
	// is the default, non-empty draws the glint. Names via
	// registry.EnchantmentName.
	Enchantments map[int32]int32

	// StoredEnchantments is minecraft:stored_enchantments, carried by
	// enchanted books alone; it travels the way Enchantments does.
	StoredEnchantments                       map[int32]int32
	enchantmentOrder, storedEnchantmentOrder []int32

	// CustomName is minecraft:custom_name, what the client draws instead of
	// the item's own name; nil for every stack but a renamed one.
	CustomName *Text

	// RepairCost is minecraft:repair_cost, the anvil work already spent and
	// what the next repair's level cost grows from; zero is the default.
	RepairCost int32

	// GlintOverride is minecraft:enchantment_glint_override, three-valued
	// because presence is part of the value; unset leaves the item's
	// default.
	GlintOverride int8

	// Unbreakable is minecraft:unbreakable; its wire value is the unit, so
	// a stack carrying it carries nothing with the component but the type.
	Unbreakable bool

	// Intangible is minecraft:intangible_projectile, set on loaded ammo that
	// was not taken from the shooter; the arrow it becomes is a creative-only
	// pickup. No network codec, like Lock.
	// vanilla: DataComponents.INTANGIBLE_PROJECTILE
	Intangible bool

	// Removed is the set of prototype components the stack's patch removes.
	// vanilla: PatchedDataComponentMap.set, DataComponentPatch removed entries
	Removed Removals

	// Container is minecraft:container, a shulker box's slots; nil is
	// absence, and entries run only to the last filled one, as stored.
	Container []ItemStack

	// Lock is minecraft:lock, the item predicate a placed container needs
	// to open; nil is absence. It has no network codec, so it lives only in
	// the server's slots and saved files.
	// vanilla: DataComponents.LOCK, LockCode.CODEC
	Lock nbt.CompoundTag

	// intangibleLoaded marks, per charged projectile, the ones that carry
	// Intangible, which the raw-held charged_projectiles bytes cannot; nil
	// when none does. ChargedProjectiles and SetChargedProjectiles keep it.
	intangibleLoaded []bool

	// DebugStick is minecraft:debug_stick_state, the property last selected
	// per block name. No network codec, like Lock. A holder replaces the map
	// rather than writing into it.
	// vanilla: DataComponents.DEBUG_STICK_STATE, DebugStickState.CODEC
	DebugStick      map[string]string
	debugStickOrder []string

	// Recipes is minecraft:recipes, the recipe names a knowledge book
	// unlocks; nil is absence. No network codec, like Lock.
	// vanilla: DataComponents.RECIPES
	Recipes []string

	// ContainerLoot is minecraft:container_loot; no network codec, like Lock.
	ContainerLoot ContainerLoot

	// MapDecorations are target markers encoded as NBT on disk and on the wire.
	// SetMapDecoration retains traversal and isolates an edit from copied stacks.
	MapDecorations     map[string]MapDecoration
	mapDecorationOrder []string

	// Raw holds the unmodelled components the wire codecs read (a potion's
	// contents, a painting's variant) as the checked bytes they arrived in,
	// which go back on the wire unchanged and into saved files under a key
	// of this server's own. They can be read only where the wire bounds each
	// value: the untrusted Set Creative Mode Slot shape, or inside a value it
	// bounds (container or bundle entries).
	Raw []RawComponent

	// patchOrder is the component types in the order the patch gained them,
	// the native traversal of the root. It is history only: presence comes
	// from the fields above, and Same ignores it. Writers that go through
	// putRaw, dropRaw, the Set methods and the readers note it; a field
	// written directly (Damage, RepairCost) is not noted, and its component
	// falls after the noted ones.
	// vanilla: DataComponentPatch
	patchOrder []int32

	// stackIdentity is a server-only identity tag for one stack, zero on
	// everything read from file, wire or registry data. It follows copies,
	// but never reaches either representation.
	stackIdentity uint64
}

// RawComponent is a data component this server does not model, kept as the
// bytes its value arrived in.
type RawComponent struct {
	// Type is the component's ID in the minecraft:data_component_type
	// registry, which is version-bound like the item IDs.
	Type int32

	// Data is the component's value, everything that follows the type on
	// the wire. Empty for a component whose value is the unit.
	Data []byte

	// Immutable decoded damage components follow stack copies; Data remains
	// the serialization authority. Replacing a component replaces its snapshot.
	damage *damageComponents
	// Native save traversal differs from the wire codec's open hash map.
	blockState *nbt.OrderedCompound
}

// Equal reports whether two stacks are identical: same item, count and
// components. The comparison a merge wants, counts aside, is Same.
func (s ItemStack) Equal(other ItemStack) bool {
	return s.Count == other.Count && s.Same(other)
}

// Empty reports whether the slot holds nothing.
func (s ItemStack) Empty() bool { return s.Count <= 0 }

// WithStackIdentity returns a copy carrying the identity tag.
func (s ItemStack) WithStackIdentity(id uint64) ItemStack {
	s.stackIdentity = id
	return s
}

// StackIdentity is the stack's server-only identity tag.
func (s ItemStack) StackIdentity() uint64 { return s.stackIdentity }

// Enchanted reports whether the stack holds any enchantment, which draws the
// glint absent an override; stored enchantments do not count.
// vanilla: ItemStack.isEnchanted
func (s ItemStack) Enchanted() bool { return len(s.Enchantments) > 0 }

// Same reports whether two stacks are the same item with the same
// components, counts aside: the test behind every merge.
// vanilla: ItemStack.isSameItemSameComponents
func (s ItemStack) Same(other ItemStack) bool {
	return s.SamePtr(&other)
}

// SamePtr is Same without copying either stack by value.
// vanilla: ItemStack.isSameItemSameComponents
func (s *ItemStack) SamePtr(other *ItemStack) bool {
	if s.ItemID != other.ItemID {
		return false
	}
	if s.Damage != other.Damage || s.RepairCost != other.RepairCost ||
		s.GlintOverride != other.GlintOverride || s.Unbreakable != other.Unbreakable ||
		s.Intangible != other.Intangible || !slices.Equal(s.intangibleLoaded, other.intangibleLoaded) ||
		s.Removed != other.Removed {
		return false
	}
	if s.CustomName != other.CustomName {
		if s.CustomName == nil || other.CustomName == nil || !namesEqual(*s.CustomName, *other.CustomName) {
			return false
		}
	}
	if len(s.Enchantments) != 0 || len(other.Enchantments) != 0 {
		if !sameEnchantments(s.Enchantments, other.Enchantments) {
			return false
		}
	}
	if len(s.Raw) != 0 || len(other.Raw) != 0 {
		if !sameRaw(s.Raw, other.Raw) {
			return false
		}
	}
	if s.Lock != nil || other.Lock != nil {
		if !nbt.Equal(s.Lock, other.Lock) {
			return false
		}
	}
	if len(s.DebugStick) != 0 || len(other.DebugStick) != 0 {
		if !reflect.DeepEqual(s.DebugStick, other.DebugStick) {
			return false
		}
	}
	if len(s.MapDecorations) != 0 || len(other.MapDecorations) != 0 {
		if !maps.Equal(s.MapDecorations, other.MapDecorations) {
			return false
		}
	}
	if s.ContainerLoot != other.ContainerLoot || !slices.Equal(s.Recipes, other.Recipes) {
		return false
	}
	if (s.Container != nil) != (other.Container != nil) {
		return false
	}
	if len(s.Container) != 0 || len(other.Container) != 0 {
		if !sameContainer(s.Container, other.Container) {
			return false
		}
	}
	if len(s.StoredEnchantments) != 0 || len(other.StoredEnchantments) != 0 {
		return sameEnchantments(s.StoredEnchantments, other.StoredEnchantments)
	}
	return true
}

// sameRaw reports whether two unmodelled component sets hold the same
// components byte for byte, in any order.
func sameRaw(a, b []RawComponent) bool {
	if len(a) != len(b) {
		return false
	}
	for _, ra := range a {
		found := false
		for _, rb := range b {
			if ra.Type == rb.Type && (bytes.Equal(ra.Data, rb.Data) || sameBlockStateProperties(ra, rb)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sameContainer compares two containers as the components they are: both
// absent-or-empty is both empty, every entry counted as the stack it is.
func sameContainer(a, b []ItemStack) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// sameEnchantments compares two enchantment maps as the components they
// are: both empty is both absent.
func sameEnchantments(a, b map[int32]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for id, level := range a {
		if other, ok := b[id]; !ok || other != level {
			return false
		}
	}
	return true
}

// TextEqual reports whether two components are the same field for field.
func TextEqual(a, b Text) bool { return namesEqual(a, b) }

// namesEqual compares two custom names field for field: the same
// translation with different arguments is a different name, however they
// render.
func namesEqual(a, b Text) bool {
	if a.Raw != nil || b.Raw != nil {
		return a.Raw != nil && b.Raw != nil && nbt.Equal(a.Raw, b.Raw)
	}
	if a.Text != b.Text || a.Translate != b.Translate || a.Color != b.Color ||
		a.Bold != b.Bold || a.Italic != b.Italic || a.Underline != b.Underline ||
		a.Insertion != b.Insertion || a.ClickCommand != b.ClickCommand || a.ClickCopy != b.ClickCopy || a.ClickURL != b.ClickURL ||
		a.ClickRun != b.ClickRun || a.ClickPage != b.ClickPage ||
		len(a.With) != len(b.With) || len(a.Extra) != len(b.Extra) {
		return false
	}
	for i := range a.With {
		if !namesEqual(a.With[i], b.With[i]) {
			return false
		}
	}
	for i := range a.Extra {
		if !namesEqual(a.Extra[i], b.Extra[i]) {
			return false
		}
	}
	if (a.HoverEntity == nil) != (b.HoverEntity == nil) ||
		(a.HoverItem == nil) != (b.HoverItem == nil) ||
		(a.HoverText == nil) != (b.HoverText == nil) {
		return false
	}
	if a.HoverText != nil && !namesEqual(*a.HoverText, *b.HoverText) {
		return false
	}
	if a.Number == nil || b.Number == nil {
		if (a.Number == nil) != (b.Number == nil) {
			return false
		}
	} else if *a.Number != *b.Number {
		return false
	}
	if a.Float == nil || b.Float == nil {
		if (a.Float == nil) != (b.Float == nil) {
			return false
		}
	} else if *a.Float != *b.Float {
		return false
	}
	if a.HoverEntity != nil && !namesEqual(a.HoverEntity.Name, b.HoverEntity.Name) {
		return false
	}
	if a.HoverEntity != nil &&
		(a.HoverEntity.ID != b.HoverEntity.ID || a.HoverEntity.UUID != b.HoverEntity.UUID) {
		return false
	}
	if a.HoverItem != nil &&
		(a.HoverItem.ID != b.HoverItem.ID || a.HoverItem.Count != b.HoverItem.Count ||
			len(a.HoverItem.Components) != len(b.HoverItem.Components)) {
		return false
	}
	return true
}

// PutComponentData writes one component's data as the full Slot format puts
// it after the type: the one place a component's wire shape is written, and
// what hashed slots checksum. A Raw component rides as its bytes.
func (b *Buffer) PutComponentData(s ItemStack, component int32) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			b.PutRaw(raw.Data)
			return
		}
	}
	switch component {
	case DataComponentMapDecorations:
		data, _ := nbt.EncodeNetwork(s.MapDecorationsTag())
		b.PutRaw(data)
	case DataComponentDamage:
		b.PutVarInt(s.Damage)
	case DataComponentUnbreakable:
		// The unit: the component's presence is all it says.
	case DataComponentCustomName:
		b.PutText(*s.CustomName)
	case DataComponentEnchantments, DataComponentStoredEnchantments:
		levels := s.Enchantments
		if component == DataComponentStoredEnchantments {
			levels = s.StoredEnchantments
		}
		ids := s.EnchantmentIDs(component == DataComponentStoredEnchantments)
		b.PutVarInt(int32(len(ids)))
		for _, id := range ids {
			b.PutVarInt(id)
			b.PutVarInt(levels[id])
		}
	case DataComponentRepairCost:
		b.PutVarInt(s.RepairCost)
	case DataComponentEnchantmentGlintOverride:
		b.PutBool(s.GlintOverride == GlintShown)
	case DataComponentContainer:
		// The list runs to the last filled slot: empty slots before it are
		// entries with only their absent flag, those after are not sent.
		length := 0
		for i, entry := range s.Container {
			if !entry.Empty() {
				length = i + 1
			}
		}
		b.PutVarInt(int32(length))
		for i := range length {
			entry := ItemStack{}
			if i < len(s.Container) {
				entry = s.Container[i]
			}
			if entry.Empty() {
				b.PutBool(false)
				continue
			}
			// A container slot's template: item, count and patch, without
			// the zero count of an empty slot.
			b.PutBool(true)
			b.PutVarInt(entry.ItemID)
			b.PutVarInt(entry.Count)
			components := componentsOf(entry)
			entry.orderPatch(components)
			b.PutVarInt(int32(len(components)))
			b.PutVarInt(entry.Removed.count())
			for _, component := range components {
				b.PutVarInt(component)
				b.PutComponentData(entry, component)
			}
			b.putRemovedComponents(entry)
		}
	}
}

// componentsOf lists the components a stack carries away from its item's
// defaults, in wire order; the wire only carries what differs.
func componentsOf(s ItemStack) []int32 {
	var ids []int32
	if len(s.MapDecorations) > 0 {
		ids = append(ids, DataComponentMapDecorations)
	}
	if s.Damage > 0 {
		ids = append(ids, DataComponentDamage)
	}
	if s.Unbreakable {
		ids = append(ids, DataComponentUnbreakable)
	}
	if s.CustomName != nil {
		ids = append(ids, DataComponentCustomName)
	}
	if len(s.Enchantments) > 0 {
		ids = append(ids, DataComponentEnchantments)
	}
	if s.RepairCost > 0 {
		ids = append(ids, DataComponentRepairCost)
	}
	if s.GlintOverride != GlintUnset {
		ids = append(ids, DataComponentEnchantmentGlintOverride)
	}
	if len(s.StoredEnchantments) > 0 {
		ids = append(ids, DataComponentStoredEnchantments)
	}
	if s.Container != nil {
		ids = append(ids, DataComponentContainer)
	}
	for _, raw := range s.Raw {
		ids = append(ids, raw.Type)
	}
	return ids
}

// putRemovedComponents writes only the removed IDs, after the added values.
// vanilla: DataComponentPatch.createStreamCodec
func (b *Buffer) putRemovedComponents(s ItemStack) {
	if s.Removed == (Removals{}) {
		return
	}
	// A single removal has nothing to order; emit it directly to skip zeroing
	// the full removable-length buffer.
	if w0, w1 := s.Removed[0], s.Removed[1]; w1 == 0 && w0&(w0-1) == 0 {
		b.PutVarInt(removable[bits.TrailingZeros64(w0)].Type)
		return
	} else if w0 == 0 && w1&(w1-1) == 0 {
		b.PutVarInt(removable[64+bits.TrailingZeros64(w1)].Type)
		return
	}
	var buf [len(removable)]int32
	types := buf[:0]
	for i, word := range s.Removed {
		for word != 0 {
			types = append(types, removable[i*64+bits.TrailingZeros64(word)].Type)
			word &= word - 1
		}
	}
	s.orderPatch(types)
	for _, t := range types {
		b.PutVarInt(t)
	}
}

// PutSlot writes an item stack: a zero byte when empty, else count, item,
// the two component counts, then the added (type and data) and removed
// arrays.
func (b *Buffer) PutSlot(s ItemStack) {
	if s.Empty() {
		b.PutVarInt(0)
		return
	}
	b.PutVarInt(s.Count)
	b.PutVarInt(s.ItemID)
	components := componentsOf(s)
	s.orderPatch(components)
	b.PutVarInt(int32(len(components)))
	b.PutVarInt(s.Removed.count())
	for _, component := range components {
		b.PutVarInt(component)
		b.PutComponentData(s, component)
	}
	b.putRemovedComponents(s)
}

// PutUntrustedSlot writes the Set Creative Mode Slot shape: PutSlot's, with
// each added value prefixed by its byte length so a reader can skip one it
// does not trust.
func (b *Buffer) PutUntrustedSlot(s ItemStack) {
	if s.Empty() {
		b.PutVarInt(0)
		return
	}
	b.PutVarInt(s.Count)
	b.PutVarInt(s.ItemID)
	components := componentsOf(s)
	s.orderPatch(components)
	b.PutVarInt(int32(len(components)))
	b.PutVarInt(s.Removed.count())
	for _, component := range components {
		var value Buffer
		value.PutComponentData(s, component)
		b.PutVarInt(component)
		b.PutVarInt(int32(value.Len()))
		b.PutRaw(value.Bytes())
	}
	b.putRemovedComponents(s)
}

// ReadSlot reads an item stack. The second result is false for a stack with
// a component that is neither modelled nor raw-held: a component's shape
// depends on its type, so an unknown one hides where the stack ends and the
// caller must drop the whole (length-prefixed) packet. ReadUntrustedSlot's
// shape prefixes each value with its length, which is how a stack with an
// unmodelled component gets in at all.
//
// An enchantment ID the client's registry lacks, or a level the codec would
// not read back, is refused too. Wear past the item's max is not refused:
// breaking the item is the server's job, not the reader's.
func (r *Reader) ReadSlot() (ItemStack, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil {
		return ItemStack{}, false
	}
	if count <= 0 {
		return ItemStack{}, true
	}
	return readSlotBody(r, count, 0)
}

// maxComponentDepth bounds component nesting (a bundle in a bundle, a
// container entry holding either). Vanilla recurses as deep as the bytes
// allow on a Java stack; a Go stack cannot. 512 matches NBT's depth: above
// the 17 a survival shulker box of bundles reaches and the couple hundred
// vanilla takes.
const maxComponentDepth = 512

// ErrSlotTooDeep reports components nested past maxComponentDepth. Each level
// is a few wire bytes but a decode frame, so it fails the whole packet, the
// caller's cue to end the connection.
var ErrSlotTooDeep = errors.New("item components nested deeper than the limit")

// readSlotBody reads a slot after its count. The caller reads the count
// because the full Slot format puts it before the item, a container entry
// after.
func readSlotBody(r *Reader, count int32, depth int) (ItemStack, bool) {
	stack := ItemStack{Count: count, ItemID: r.ReadVarInt()}
	return readSlotPatch(r, stack, depth)
}

// readSlotPatch reads the added and removed components a stack ends with.
func readSlotPatch(r *Reader, stack ItemStack, depth int) (ItemStack, bool) {
	if depth > maxComponentDepth {
		r.fail(ErrSlotTooDeep)
		return ItemStack{}, false
	}
	add, remove := r.ReadVarInt(), r.ReadVarInt()
	for range add {
		component := r.ReadVarInt()
		if r.Err() != nil {
			return ItemStack{}, false
		}
		if !readComponentData(r, &stack, component, depth) {
			return ItemStack{}, false
		}
		if r.Err() != nil {
			return ItemStack{}, false
		}
		stack.NotePatch(component)
	}
	if !readRemovedComponents(r, &stack, remove) {
		return ItemStack{}, false
	}
	if r.Err() != nil {
		return ItemStack{}, false
	}
	return stack, true
}

// ReadUntrustedSlot reads the Set Creative Mode Slot shape, where each added
// value is prefixed by its byte length and read from a bounded slice. An
// unmodelled component is kept in Raw after a check against its codec; one
// no codec knows, a modelled one that does not read, or a slice longer than
// the packet is refused.
func (r *Reader) ReadUntrustedSlot() (ItemStack, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil {
		return ItemStack{}, false
	}
	if count <= 0 {
		return ItemStack{}, true
	}

	stack := ItemStack{Count: count, ItemID: r.ReadVarInt()}
	add, remove := r.ReadVarInt(), r.ReadVarInt()
	refused := false
	for range add {
		component := r.ReadVarInt()
		if r.Err() != nil {
			return ItemStack{}, false
		}
		if readLengthPrefixedComponent(r, &stack, component, 0) {
			stack.NotePatch(component)
		} else {
			// The slice is spent either way, so the rest of the patch is
			// read before the stack is refused: the caller gets a whole
			// slot, not leftover bytes. A short slice or the nesting
			// refusal is a decode failure and stops the read.
			if r.Err() != nil {
				return ItemStack{}, false
			}
			refused = true
		}
	}
	if !readRemovedComponents(r, &stack, remove) {
		return ItemStack{}, false
	}
	if r.Err() != nil || refused {
		return ItemStack{}, false
	}
	return stack, true
}

// readLengthPrefixedComponent reads one length-prefixed added component
// from a reader over its slice alone. Bytes the codec does not spend are
// left behind in the slice, as the game leaves them, so a raw component is
// kept as the bytes its codec read.
func readLengthPrefixedComponent(r *Reader, stack *ItemStack, component int32, depth int) bool {
	length := r.ReadVarInt()
	if r.Err() != nil {
		return false
	}
	if length < 0 {
		// A negative length fails the packet: there is no telling where
		// the next component starts.
		r.fail(errors.New("component value length is negative"))
		return false
	}
	value := r.take(int(length))
	if r.Err() != nil {
		return false
	}
	inner := NewReader(value)
	if ok := readComponentData(inner, stack, component, depth); !ok {
		// The nesting refusal has to leave the value slice: the packet
		// failed the decode, not only the one component.
		if errors.Is(inner.Err(), ErrSlotTooDeep) {
			r.fail(ErrSlotTooDeep)
		}
		return false
	}
	return true
}

// readRemovedComponents keeps supported removals and clears values for the
// other known types. A type outside the registry is refused.
func readRemovedComponents(r *Reader, stack *ItemStack, remove int32) bool {
	for range remove {
		component := r.ReadVarInt()
		if r.Err() != nil || component < 0 || component >= DataComponentTypeCount {
			return false
		}
		if stack.Remove(component) {
			continue
		}
		switch component {
		case DataComponentUnbreakable:
			stack.Unbreakable = false
		case DataComponentCustomName:
			stack.CustomName = nil
		default:
			stack.clearRaw(component)
		}
		stack.NotePatch(component)
	}
	return true
}

// dropRaw takes one unmodelled component back out of the stack, the way a
// removal takes a modelled one back to its item's default.
func (s *ItemStack) dropRaw(component int32) {
	s.ForgetPatch(component)
	s.clearRaw(component)
}

// clearRaw clears the raw payload of component but keeps its place in the
// patch history, for a removal or replacement that stays in the patch.
func (s *ItemStack) clearRaw(component int32) {
	if component == DataComponentChargedProjectiles {
		s.intangibleLoaded = nil
	}
	kept := s.Raw[:0:0]
	for _, raw := range s.Raw {
		if raw.Type != component {
			kept = append(kept, raw)
		}
	}
	s.Raw = kept
}

// putRaw sets an unmodelled component, replacing one of its type: the
// wire's patch is a map, where the last value of a type wins.
func (s *ItemStack) putRaw(raw RawComponent) {
	raw.decodeDamageComponents()
	raw.decodeBlockStateProperties()
	s.clearRemoval(raw.Type)
	s.NotePatch(raw.Type)
	for i := range s.Raw {
		if s.Raw[i].Type == raw.Type {
			s.Raw[i] = raw
			return
		}
	}
	s.Raw = append(s.Raw, raw)
}

// readComponentData reads one added component's data, reporting whether this
// server can represent it. A malformed modelled one refuses the stack
// without failing the read; a raw-held one is checked against its codec and
// kept; any other type is refused, so relaying cannot hand the corruption
// to every client the stack reaches.
func readComponentData(r *Reader, stack *ItemStack, component int32, depth int) bool {
	switch component {
	case DataComponentMapDecorations:
		if r.err != nil {
			return false
		}
		tag, n, err := nbt.DecodeNetworkOrderedQuota(r.data[r.pos:], nbt.NetworkQuota)
		if err != nil {
			r.fail(err)
			return false
		}
		r.pos += n
		if !stack.SetMapDecorations(tag) {
			return false
		}
	case DataComponentDamage:
		damage := r.ReadVarInt()
		if damage < 0 {
			return false
		}
		stack.SetDamage(damage)
	case DataComponentUnbreakable:
		stack.Unbreakable = true
	case DataComponentCustomName:
		name, ok := ReadText(r)
		if !ok {
			return false
		}
		stack.CustomName = &name
	case DataComponentEnchantments, DataComponentStoredEnchantments:
		levels, ids, ok := readEnchantments(r)
		if !ok {
			return false
		}
		stack.SetEnchantments(component == DataComponentStoredEnchantments, levels, ids...)
	case DataComponentRepairCost:
		cost := r.ReadVarInt()
		if cost < 0 {
			return false
		}
		stack.SetRepairCost(cost)
	case DataComponentEnchantmentGlintOverride:
		switch r.ReadBool() {
		case true:
			stack.GlintOverride = GlintShown
		default:
			stack.GlintOverride = GlintHidden
		}
	case DataComponentContainer:
		contents, ok := readContainerContents(r, depth)
		if !ok {
			return false
		}
		stack.Container = contents
	default:
		return readRawComponent(r, stack, component, depth)
	}
	return true
}

// readContainerContents reads a length, then that many entries, each a
// present slot template or an absent flag. The codec binds the list to 256,
// and a present template must hold something: reading it as empty would
// lose what the flag says.
func readContainerContents(r *Reader, depth int) ([]ItemStack, bool) {
	length := r.ReadVarInt()
	if r.Err() != nil || length < 0 || length > 256 {
		return nil, false
	}
	if length == 0 {
		return []ItemStack{}, true
	}
	contents := make([]ItemStack, length)
	for i := range length {
		if !r.ReadBool() {
			continue
		}
		item, count := r.ReadVarInt(), r.ReadVarInt()
		if count < 1 {
			return nil, false
		}
		entry, ok := readSlotPatch(r, ItemStack{ItemID: item, Count: count}, depth+1)
		if !ok {
			return nil, false
		}
		contents[i] = entry
	}
	return contents, true
}

// readEnchantments reads a count, then pairs of registry ID and level. A map
// is built even for one entry, since components are compared and stored
// whole.
func readEnchantments(r *Reader) (map[int32]int32, []int32, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, nil, false
	}
	if count == 0 {
		// An empty component is its own default, and reads as absent.
		return nil, nil, true
	}
	// Vanilla presizes at min(count, 65536). Each entry is at least two
	// varints, so a count the bytes cannot back fails anyway; presizing past
	// that would only hand a lying packet memory.
	levels := make(map[int32]int32, min(count, int32(r.Remaining()/2)))
	ids := make([]int32, 0, len(levels))
	for range count {
		id, level := r.ReadVarInt(), r.ReadVarInt()
		if r.Err() != nil || id < 0 || level < 1 || level > 255 {
			return nil, nil, false
		}
		if _, present := levels[id]; !present {
			ids = append(ids, id)
		}
		levels[id] = level
	}
	return levels, ids, true
}

// ReadText reads a custom name: a bare string tag for a plain component, a
// compound otherwise. It reports false for a component the game's codec
// would refuse or this model cannot hold whole, which would take a reading
// client down with the stack. Packet NBT has no length, so it is decoded
// from the unread payload and the reader moved past it.
func ReadText(r *Reader) (Text, bool) {
	return readText(r, false)
}

// ReadComponentText is ReadText for a component the stack relays whole, a
// book page: one the model has no field for (a selector, a score) is kept as
// its tag when the codec reads it.
// vanilla: ComponentSerialization.TRUSTED_CONTEXT_FREE_STREAM_CODEC
func ReadComponentText(r *Reader) (Text, bool) {
	return readText(r, true)
}

func readText(r *Reader, whole bool) (Text, bool) {
	if r.err != nil {
		return Text{}, false
	}
	tag, n, err := nbt.DecodeNetworkQuota(r.data[r.pos:], nbt.NetworkQuota)
	if err != nil {
		r.fail(err)
		return Text{}, false
	}
	r.pos += n
	if whole {
		return ComponentTextOf(tag)
	}
	return StyledTextOf(tag)
}

// HashedSlot is a stack as Click Container reports it, its components as
// CRC32C checksums. They cannot be read back, so Matches compares them
// against the real slot; removed components carry no checksum and only
// expect the default.
type HashedSlot struct {
	// Stack holds the item and count; its components stay default, the
	// checksums below standing in for them.
	Stack ItemStack

	// hashes are the checksums the claim carried for the modelled
	// components it named, keyed by component type; nil for a plain stack.
	hashes map[int32]int32
}

// Matches reports whether a hashed claim agrees with the real slot: same item
// and count, a checksum for each component it carries and none for one it
// lacks. A checksum this server cannot compute (an unnamed enchantment)
// counts as a disagreement, so the slot is corrected.
func (h HashedSlot) Matches(stack ItemStack) bool {
	if h.Stack.ItemID != stack.ItemID || h.Stack.Count != stack.Count {
		return false
	}
	components := componentsOf(stack)
	if len(components) != len(h.hashes) {
		return false
	}
	for _, component := range components {
		checksum, computable := componentChecksumOf(stack, component)
		if !computable || int32Le(checksum) != h.hashes[component] {
			return false
		}
	}
	return true
}

// ReadHashedSlot reads Click Container's hashed slot, what the client
// believes a slot or its cursor holds. Unlike ReadSlot's shape, a Has Item
// boolean comes first, the count follows the item ID, and each CRC32C
// collection is prefixed by its own count.
//
// The second result is false for a claim this server cannot compare (an
// unmodelled or unreadable component) or a count outside
// 0..HashedPatchLimit. A readable claim that disagrees is the caller's to
// correct through HashedSlot.Matches.
func (r *Reader) ReadHashedSlot() (HashedSlot, bool) {
	hasItem := r.ReadBool()
	if r.Err() != nil || !hasItem {
		return HashedSlot{}, true
	}
	claim := HashedSlot{Stack: ItemStack{ItemID: r.ReadVarInt(), Count: r.ReadVarInt()}}
	comparable := true
	added := r.ReadVarInt()
	if r.Err() != nil {
		return HashedSlot{}, false
	}
	// Both counts are bounded at 256 by the codec's map and set; a negative
	// count fails its capacity allocation.
	// vanilla: HashedPatchMap.STREAM_CODEC
	if added < 0 || added > HashedPatchLimit {
		r.fail(fmt.Errorf("hashed slot adds %d components, at most %d", added, HashedPatchLimit))
		return HashedSlot{}, false
	}
	for range added { // the added components' checksums
		component, hash := r.ReadVarInt(), r.ReadI32()
		if r.Err() != nil {
			return HashedSlot{}, false
		}
		switch component {
		case DataComponentDamage, DataComponentUnbreakable, DataComponentCustomName,
			DataComponentEnchantments, DataComponentRepairCost,
			DataComponentEnchantmentGlintOverride, DataComponentStoredEnchantments,
			DataComponentContainer:
			if claim.hashes == nil {
				claim.hashes = map[int32]int32{}
			}
			claim.hashes[component] = hash
		default:
			// An unmodelled component has nothing to check against. Its
			// fields are still read, so the rest of the slot is read
			// before the claim is refused.
			comparable = false
		}
	}
	removed := r.ReadVarInt()
	if r.Err() != nil {
		return HashedSlot{}, false
	}
	if removed < 0 || removed > HashedPatchLimit {
		r.fail(fmt.Errorf("hashed slot removes %d components, at most %d", removed, HashedPatchLimit))
		return HashedSlot{}, false
	}
	for range removed { // the removed components' types
		component := r.ReadVarInt()
		if r.Err() != nil {
			return HashedSlot{}, false
		}
		switch component {
		case DataComponentDamage, DataComponentUnbreakable, DataComponentCustomName,
			DataComponentEnchantments, DataComponentRepairCost,
			DataComponentEnchantmentGlintOverride, DataComponentStoredEnchantments,
			DataComponentContainer:
			// A removal carries no checksum; the absence it claims is
			// what Matches reads the claim's checksums against.
		default:
			comparable = false
		}
	}
	if r.Err() != nil || !comparable {
		return HashedSlot{}, false
	}
	return claim, true
}
