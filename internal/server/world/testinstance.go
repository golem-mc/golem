package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Rotation.CODEC, TestInstanceBlockEntity.Status
var (
	testInstanceRotations = [...]string{"none", "clockwise_90", "180", "counterclockwise_90"}
	testInstanceStatuses  = [...]string{"cleared", "running", "finished"}
)

// TestInstanceData is the saved configuration and result of a test instance block.
type TestInstanceData struct {
	Test           string
	Size           [3]int32
	Rotation       int32
	IgnoreEntities bool
	Status         int32
	ErrorMessage   *protocol.Text
}

// ReadTestInstanceData decodes the data record. A missing required field leaves
// the caller's current data intact, or the zero value for a fresh block.
// vanilla: TestInstanceBlockEntity.Data.CODEC
func ReadTestInstanceData(tag nbt.Tag) (TestInstanceData, bool) {
	c, ok := nbt.CompoundView(tag)
	if !ok {
		return TestInstanceData{}, false
	}
	size, sized := testInstanceSize(c["size"])
	rotation, _ := nbt.Get[nbt.String](c, "rotation")
	status, _ := nbt.Get[nbt.String](c, "status")
	r := slices.Index(testInstanceRotations[:], string(rotation))
	s := slices.Index(testInstanceStatuses[:], string(status))
	ignore, flag := nbt.NumberValue(c["ignore_entities"])
	if !sized || r < 0 || s < 0 || !flag {
		return TestInstanceData{}, false
	}
	data := TestInstanceData{Size: size, Rotation: int32(r), Status: int32(s), IgnoreEntities: ignore != 0}
	if test, present := c["test"]; present {
		name, ok := test.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(name)) {
			return TestInstanceData{}, false
		}
		data.Test = registry.Qualify(string(name))
	}
	if message, present := c["error_message"]; present {
		text, ok := protocol.ComponentTextOf(message)
		if !ok {
			return TestInstanceData{}, false
		}
		data.ErrorMessage = &text
	}
	return data, true
}

// Tag writes the data record in codec order.
// vanilla: TestInstanceBlockEntity.Data.CODEC
func (d TestInstanceData) Tag() nbt.CompoundTag {
	tag := nbt.NewOrderedCompound(6)
	if d.Test != "" {
		tag.Set("test", nbt.String(d.Test))
	}
	tag.Set("size", nbt.IntArray(d.Size[:]))
	tag.Set("rotation", nbt.String(testInstanceRotations[protocol.IDWrap(d.Rotation, int32(len(testInstanceRotations)))]))
	ignore := nbt.Byte(0)
	if d.IgnoreEntities {
		ignore = 1
	}
	tag.Set("ignore_entities", ignore)
	status := d.Status
	if status < 0 || int(status) >= len(testInstanceStatuses) {
		status = 0
	}
	tag.Set("status", nbt.String(testInstanceStatuses[status]))
	if d.ErrorMessage != nil {
		tag.Set("error_message", protocol.TextTag(*d.ErrorMessage))
	}
	return tag
}

// testInstanceSize reads Vec3i's int stream; an overlong stream has a partial
// result containing its first three values.
func testInstanceSize(tag nbt.Tag) ([3]int32, bool) {
	var size [3]int32
	switch v := tag.(type) {
	case nbt.IntArray:
		return sizeFromSlice(v), len(v) >= len(size)
	case nbt.ByteArray:
		for i, b := range v[:min(len(v), len(size))] {
			size[i] = int32(int8(b))
		}
		return size, len(v) >= len(size)
	case nbt.LongArray:
		return sizeFromSlice(v), len(v) >= len(size)
	case nbt.List:
		if len(v.Items) < len(size) {
			return size, false
		}
		for i := range size {
			n, ok := nbt.IntValue(nbt.ListElement(v.Items[i]))
			if !ok {
				return size, false
			}
			size[i] = n
		}
		return size, true
	}
	return size, false
}

func sizeFromSlice[T ~int32 | ~int64](values []T) [3]int32 {
	var size [3]int32
	for i, v := range values[:min(len(values), len(size))] {
		size[i] = int32(v)
	}
	return size
}
