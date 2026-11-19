package nbt

import (
	"errors"
	"fmt"
)

// NetworkQuota is the quota a packet's NBT is read under, in Java's
// estimated memory cost (a string is 36 + 2 per UTF-16 unit).
// vanilla: NbtAccounter.create(2097152L) (ByteBufCodecs.TAG, FriendlyByteBuf.readNbt)
const NetworkQuota = 2097152

// ErrOverQuota is what DecodeNetworkQuota wraps when a tag passes its quota.
var ErrOverQuota = errors.New("nbt: tag too big for its quota")

// DecodeNetworkQuota is DecodeNetwork that charges tags as they are read and
// refuses one passing quota before any of its tree is built.
// vanilla: NbtAccounter.accountBytes
func DecodeNetworkQuota(data []byte, quota int64) (tag Tag, n int, err error) {
	return DecodeNetworkLimits(data, quota, maxDepth)
}

// DecodeNetworkOrderedQuota checks the quota and retains compound insertion history.
func DecodeNetworkOrderedQuota(data []byte, quota int64) (tag Tag, n int, err error) {
	return decodeNetworkLimits(data, quota, maxDepth, true)
}

// DecodeNetworkLimits enforces quota and container depth before decoding or unwrapping lists.
// vanilla: NbtAccounter.pushDepth, ListTag.TYPE.load, CompoundTag.TYPE.load
func DecodeNetworkLimits(data []byte, quota int64, depthLimit int) (tag Tag, n int, err error) {
	return decodeNetworkLimits(data, quota, depthLimit, false)
}

func decodeNetworkLimits(data []byte, quota int64, depthLimit int, ordered bool) (tag Tag, n int, err error) {
	a := &accounter{data: data, quota: quota, depthLimit: min(depthLimit, maxDepth)}
	tagType := Type(a.u8())
	if a.err == nil && tagType != TypeEnd {
		a.payload(tagType, 0)
	}
	if a.err != nil {
		return nil, 0, a.err
	}
	return decodeNetwork(data, ordered)
}

// accounter walks a tag's bytes with decoder's bounds checks, building
// nothing and summing vanilla's NbtAccounter charge; a shape decoder refuses
// is left for decoder to report.
//
// A repeated compound name is charged 36 again, where vanilla charges it
// once: telling a repeat apart would need the name set this walk avoids.
type accounter struct {
	data       []byte
	pos        int
	used       int64
	quota      int64
	depthLimit int
	err        error
}

// charge adds n to the count and fails the walk once it passes the quota.
func (a *accounter) charge(n int64) {
	if a.err != nil {
		return
	}
	if a.used+n > a.quota {
		a.err = fmt.Errorf("%w: tried to allocate %d + %d bytes where max allowed is %d", ErrOverQuota, a.used, n, a.quota)
		return
	}
	a.used += n
}

// skip moves over n bytes, failing the walk if there are not that many.
func (a *accounter) skip(n int) []byte {
	if a.err != nil {
		return nil
	}
	if n < 0 || n > len(a.data)-a.pos {
		a.err = fmt.Errorf("nbt: truncated at offset %d", a.pos)
		return nil
	}
	b := a.data[a.pos : a.pos+n]
	a.pos += n
	return b
}

func (a *accounter) u8() uint8 {
	if b := a.skip(1); b != nil {
		return b[0]
	}
	return 0
}

func (a *accounter) u16() int {
	if b := a.skip(2); b != nil {
		return int(b[0])<<8 | int(b[1])
	}
	return 0
}

// count reads an array or list length, checked as decoder.count does.
func (a *accounter) count(elemSize int) int64 {
	b := a.skip(4)
	if b == nil {
		return 0
	}
	n := int32(uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]))
	if n < 0 || int(n) > (len(a.data)-a.pos)/elemSize {
		a.err = fmt.Errorf("nbt: length %d at offset %d is longer than the rest of the data", n, a.pos)
		return 0
	}
	return int64(n)
}

// stringUnits skips a modified UTF-8 string and answers its UTF-16 length.
func (a *accounter) stringUnits() int64 {
	raw := a.skip(a.u16())
	var units int64
	for i := 0; i < len(raw); units++ {
		switch b := raw[i]; {
		case b&0xE0 == 0xC0:
			i += 2
		case b&0xF0 == 0xE0:
			i += 3
		default:
			i++
		}
	}
	return units
}

// payload charges one tag's payload.
// vanilla: the TagType loads (ByteTag.TYPE ... CompoundTag.TYPE)
func (a *accounter) payload(tagType Type, depth int) {
	if a.err != nil {
		return
	}
	if tagType == TypeList || tagType == TypeCompound {
		if depth >= a.depthLimit {
			a.err = fmt.Errorf("nbt: tags nested more than %d deep", a.depthLimit)
			return
		}
		depth++
	}
	switch tagType {
	case TypeByte:
		a.charge(9)
		a.skip(1)
	case TypeShort:
		a.charge(10)
		a.skip(2)
	case TypeInt, TypeFloat:
		a.charge(12)
		a.skip(4)
	case TypeLong, TypeDouble:
		a.charge(16)
		a.skip(8)
	case TypeString:
		a.charge(36)
		a.charge(2 * a.stringUnits())
	case TypeByteArray:
		a.array(1)
	case TypeIntArray:
		a.array(4)
	case TypeLongArray:
		a.array(8)
	case TypeList:
		a.charge(36)
		elem := Type(a.u8())
		n := a.count(1)
		a.charge(4 * n)
		if elem == TypeEnd {
			return // decoder refuses a list of end tags with entries
		}
		for range n {
			if a.err != nil {
				return
			}
			a.payload(elem, depth)
		}
	case TypeCompound:
		a.charge(48)
		for a.err == nil {
			fieldType := Type(a.u8())
			if a.err != nil || fieldType == TypeEnd {
				return
			}
			a.charge(28)
			a.charge(2 * a.stringUnits())
			a.payload(fieldType, depth)
			a.charge(36)
		}
	default:
		a.err = fmt.Errorf("nbt: unknown tag %d at offset %d", tagType, a.pos)
	}
}

// array charges an array of elements size bytes apiece.
func (a *accounter) array(size int) {
	a.charge(24)
	n := a.count(size)
	a.charge(int64(size) * n)
	a.skip(int(n) * size)
}
