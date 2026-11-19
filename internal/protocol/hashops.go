package protocol

import (
	"encoding/binary"
	"hash/crc32"
	"math"
	"sort"
	"unicode/utf16"

	"github.com/golem-mc/golem/nbt"
)

// crc32cTable is CRC32C — the Castagnoli polynomial the game's hash ops run
// on, not the plain CRC32 of most checksums in the wild.
var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// The hashed slot checksums. The wiki leaves them undocumented, and they are
// not CRC32C of the wire bytes: vanilla hashes each component through its
// serialization codec with HashOps over CRC32C (ServerPlayer's slot
// synchronizer), read here off the bytecode.
//
// A number hashes as its tag and little-endian bytes, a string as its tag
// and UTF-16 units, a compound as map start, entries in hashed-key order,
// map end. The claim carries the four digest bytes as a little-endian int.

// The tags a hashed value opens and closes with, one byte ahead of its
// contents.
const (
	hashTagEmpty      = 0x01
	hashTagMapStart   = 0x02
	hashTagMapEnd     = 0x03
	hashTagListStart  = 0x04
	hashTagListEnd    = 0x05
	hashTagByte       = 0x06
	hashTagShort      = 0x07
	hashTagInt        = 0x08
	hashTagLong       = 0x09
	hashTagFloat      = 0x0A
	hashTagDouble     = 0x0B
	hashTagString     = 0x0C
	hashTagBoolean    = 0x0D
	hashByteArrayTags = 0x0E // through to the long array's end tag, in steps of two
)

// EnchantmentName resolves an enchantment ID to its component name; the
// registry package installs it. Nil makes an enchantment's checksum
// uncomputable, so the slot is corrected rather than confirmed.
var EnchantmentName func(id int32) (string, bool)

// ItemName resolves an item ID to its registry name, which container
// components hash by; the registry package installs it. Nil makes a
// container's checksum uncomputable, so the slot is corrected.
var ItemName func(id int32) (string, bool)

// componentNames are the registry keys component types hash under,
// constants of this version like their IDs.
var componentNames = map[int32]string{
	DataComponentDamage:                   "minecraft:damage",
	DataComponentUnbreakable:              "minecraft:unbreakable",
	DataComponentCustomName:               "minecraft:custom_name",
	DataComponentEnchantments:             "minecraft:enchantments",
	DataComponentRepairCost:               "minecraft:repair_cost",
	DataComponentEnchantmentGlintOverride: "minecraft:enchantment_glint_override",
	DataComponentStoredEnchantments:       "minecraft:stored_enchantments",
	DataComponentContainer:                "minecraft:container",
}

// hashBuilder feeds the bytes a hashed value is made of; its sum is the
// CRC32C digest the checksums are taken from.
type hashBuilder struct {
	buf []byte
}

func (h *hashBuilder) putByte(b byte)    { h.buf = append(h.buf, b) }
func (h *hashBuilder) putRaw(raw []byte) { h.buf = append(h.buf, raw...) }

// putIntLE writes the four bytes a little-endian integer is hashed as, the
// order Guava's hasher feeds every primitive in.
func (h *hashBuilder) putIntLE(v int32) {
	h.buf = binary.LittleEndian.AppendUint32(h.buf, uint32(v))
}

func (h *hashBuilder) putLongLE(v int64) {
	h.buf = binary.LittleEndian.AppendUint64(h.buf, uint64(v))
}

// putF32LE writes the four bytes a float is hashed as, its own bit pattern.
func (h *hashBuilder) putF32LE(v float32) {
	h.buf = binary.LittleEndian.AppendUint32(h.buf, math.Float32bits(v))
}

func (h *hashBuilder) putF64LE(v float64) {
	h.buf = binary.LittleEndian.AppendUint64(h.buf, math.Float64bits(v))
}

// putString writes a string the way createString hashes one: the number of
// UTF-16 code units it is, then the units themselves, two bytes each.
func (h *hashBuilder) putString(s string) {
	units := utf16.Encode([]rune(s))
	h.putIntLE(int32(len(units)))
	for _, unit := range units {
		h.buf = binary.LittleEndian.AppendUint16(h.buf, unit)
	}
}

// sum is the digest of everything fed so far, as the four bytes the checks
// compare.
func (h *hashBuilder) sum() [4]byte {
	var d [4]byte
	binary.LittleEndian.PutUint32(d[:], crc32Checksum(h.buf))
	return d
}

func crc32Checksum(data []byte) uint32 {
	return crc32.Checksum(data, crc32cTable)
}

// hashEmptyMap is the hash of an empty compound, what the unit codecs — the
// unbreakable flag's — encode to.
func hashEmptyMap() [4]byte {
	var h hashBuilder
	h.putByte(hashTagMapStart)
	h.putByte(hashTagMapEnd)
	return h.sum()
}

// hashBool is the hash of the two booleans, which the game keeps whole
// rather than hashing anything for.
func hashBool(v bool) [4]byte {
	var h hashBuilder
	h.putByte(hashTagBoolean)
	if v {
		h.putByte(1)
	} else {
		h.putByte(0)
	}
	return h.sum()
}

func hashInt(v int32) [4]byte {
	var h hashBuilder
	h.putByte(hashTagInt)
	h.putIntLE(v)
	return h.sum()
}

func hashString(s string) [4]byte {
	var h hashBuilder
	h.putByte(hashTagString)
	h.putString(s)
	return h.sum()
}

// hashTag is one hashed field of a compound: its key and the digest of its
// value.
type hashTag struct {
	key   string
	value [4]byte
}

// hashCompound is the hash of a compound: a map start, its entries ordered by
// key digest then value digest, and a map end.
// vanilla: HashOps.MAP_ENTRY_ORDER
func hashCompound(fields []hashTag) [4]byte {
	keys := make([][4]byte, len(fields))
	order := make([]int, len(fields))
	for i, field := range fields {
		keys[i] = hashString(field.key)
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if ka, kb := uint32(int32Le(keys[a])), uint32(int32Le(keys[b])); ka != kb {
			return ka < kb
		}
		return uint32(int32Le(fields[a].value)) < uint32(int32Le(fields[b].value))
	})
	var h hashBuilder
	h.putByte(hashTagMapStart)
	for _, i := range order {
		h.putRaw(keys[i][:])
		h.putRaw(fields[i].value[:])
	}
	h.putByte(hashTagMapEnd)
	return h.sum()
}

// int32Le reads the little-endian integer a digest's bytes make; read as
// unsigned it is what the game's comparator orders hashed entries by
// (HashCode.padToLong).
func int32Le(d [4]byte) int32 {
	return int32(binary.LittleEndian.Uint32(d[:]))
}

// hashNBT hashes a tag tree as the codecs hash values: numbers, strings,
// booleans, compounds and lists.
func hashNBT(tag nbt.Tag) [4]byte {
	// A compound's digest is its hash, not an input to another one.
	if v, ok := nbt.CompoundView(tag); ok {
		fields := make([]hashTag, 0, len(v))
		for name, value := range v {
			fields = append(fields, hashTag{key: name, value: hashNBT(value)})
		}
		return hashCompound(fields)
	}
	var h hashBuilder
	hashNBTInto(&h, tag)
	return h.sum()
}

func hashNBTInto(h *hashBuilder, tag nbt.Tag) {
	switch v := tag.(type) {
	case nbt.Byte:
		// A byte is a boolean where the codec says so; the codecs here only
		// write 0 and 1, so those hash as booleans, wider ones as bytes.
		if v <= 1 {
			h.putByte(hashTagBoolean)
			h.putByte(byte(v))
			return
		}
		h.putByte(hashTagByte)
		h.putByte(byte(v))
	case nbt.Short:
		h.putByte(hashTagShort)
		h.buf = binary.LittleEndian.AppendUint16(h.buf, uint16(v))
	case nbt.Int:
		h.putByte(hashTagInt)
		h.putIntLE(int32(v))
	case nbt.Long:
		h.putByte(hashTagLong)
		h.putLongLE(int64(v))
	case nbt.Float:
		h.putByte(hashTagFloat)
		h.putF32LE(float32(v))
	case nbt.Double:
		h.putByte(hashTagDouble)
		h.putF64LE(float64(v))
	case nbt.String:
		h.putByte(hashTagString)
		h.putString(string(v))
	case nbt.ByteArray:
		h.putByte(hashByteArrayTags) // the byte array's start
		h.putRaw(v)
		h.putByte(hashByteArrayTags + 1) // and its end
	case nbt.IntArray:
		h.putByte(hashByteArrayTags + 2) // the int array's start
		for _, value := range v {
			h.putIntLE(value)
		}
		h.putByte(hashByteArrayTags + 3) // and its end
	case nbt.LongArray:
		h.putByte(hashByteArrayTags + 4) // the long array's start
		for _, value := range v {
			h.putLongLE(value)
		}
		h.putByte(hashByteArrayTags + 5) // and its end
	case nbt.List:
		h.putByte(hashTagListStart)
		for _, item := range v.Items {
			digest := hashNBT(item)
			h.putRaw(digest[:])
		}
		h.putByte(hashTagListEnd)
	}
}

// componentChecksumOf hashes one component's codec-encoded value; false when
// it cannot be computed, the cue to correct the slot.
func componentChecksumOf(s ItemStack, component int32) ([4]byte, bool) {
	switch component {
	case DataComponentDamage:
		return hashInt(s.Damage), true
	case DataComponentUnbreakable:
		// The flag's value is the unit, which the codec encodes as the
		// empty compound.
		return hashEmptyMap(), true
	case DataComponentCustomName:
		return hashNBT(TextTag(*s.CustomName)), true
	case DataComponentEnchantments, DataComponentStoredEnchantments:
		levels := s.Enchantments
		if component == DataComponentStoredEnchantments {
			levels = s.StoredEnchantments
		}
		if len(levels) == 0 {
			return hashEmptyMap(), true
		}
		fields := make([]hashTag, 0, len(levels))
		for id, level := range levels {
			name, ok := enchantmentNameOf(id)
			if !ok {
				return [4]byte{}, false
			}
			fields = append(fields, hashTag{key: name, value: hashInt(level)})
		}
		return hashCompound(fields), true
	case DataComponentRepairCost:
		return hashInt(s.RepairCost), true
	case DataComponentEnchantmentGlintOverride:
		return hashBool(s.GlintOverride == GlintShown), true
	case DataComponentContainer:
		return hashContainer(s.Container)
	default:
		return [4]byte{}, false
	}
}

// hashContainer hashes a list of one record per filled slot, in slot order:
// the slot and the stack as its template.
func hashContainer(contents []ItemStack) ([4]byte, bool) {
	var h hashBuilder
	h.putByte(hashTagListStart)
	for i, entry := range contents {
		if entry.Empty() {
			continue
		}
		template, ok := hashItemTemplate(entry)
		if !ok {
			return [4]byte{}, false
		}
		digest := hashCompound([]hashTag{
			{key: "slot", value: hashInt(int32(i))},
			{key: "item", value: template},
		})
		h.putRaw(digest[:])
	}
	h.putByte(hashTagListEnd)
	return h.sum(), true
}

// hashItemTemplate hashes the item name, the count when not one, and the
// non-default components; anything unhashable makes it uncomputable.
func hashItemTemplate(entry ItemStack) ([4]byte, bool) {
	if ItemName == nil {
		return [4]byte{}, false
	}
	name, ok := ItemName(entry.ItemID)
	if !ok {
		return [4]byte{}, false
	}
	fields := []hashTag{{key: "id", value: hashString(name)}}
	if entry.Count != 1 {
		fields = append(fields, hashTag{key: "count", value: hashInt(entry.Count)})
	}
	components := componentsOf(entry)
	if len(components) > 0 {
		componentFields := make([]hashTag, 0, len(components))
		for _, component := range components {
			componentName, ok := componentNames[component]
			if !ok {
				return [4]byte{}, false
			}
			digest, ok := componentChecksumOf(entry, component)
			if !ok {
				return [4]byte{}, false
			}
			componentFields = append(componentFields, hashTag{key: componentName, value: digest})
		}
		fields = append(fields, hashTag{key: "components", value: hashCompound(componentFields)})
	}
	return hashCompound(fields), true
}

// enchantmentNameOf is the name lookup through the hook the registry
// installs, nil-safe for a protocol package used without one.
func enchantmentNameOf(id int32) (string, bool) {
	if EnchantmentName == nil {
		return "", false
	}
	return EnchantmentName(id)
}
