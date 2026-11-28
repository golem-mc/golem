package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// customName is a body's CustomName and CustomNameVisible, the part of
// vanilla's Entity that every kind saves and synchs; the zero value is no
// name, hidden. vanilla: Entity.saveWithoutId, Entity.load
type customName struct {
	// wire is the name as network NBT, "" for none. A string keeps the
	// value comparable, so a tracker's metadata record can hold it.
	wire    string
	visible bool
}

// customNameOfTag reads a compound's name and visibility; a name the component
// codec refuses leaves the body unnamed.
func customNameOfTag(compound nbt.Compound) customName {
	n := customName{visible: nbt.BoolField(compound, "CustomNameVisible")}
	if name, ok := protocol.LenientTextOf(compound["CustomName"]); ok {
		if data, err := nbt.EncodeNetwork(protocol.TextTag(name)); err == nil {
			n.wire = string(data)
		}
	}
	return n
}

// customNameOfStack is the name a body made from the stack takes: the stack's
// custom name, hidden.
// vanilla: Entity.applyImplicitComponents
func customNameOfStack(stack protocol.ItemStack) customName {
	if stack.CustomName == nil {
		return customName{}
	}
	data, _ := nbt.EncodeNetwork(protocol.TextTag(*stack.CustomName))
	return customName{wire: string(data)}
}

// tag is the name as a CustomName tag, nil for none.
func (n customName) tag() nbt.Tag {
	if n.wire == "" {
		return nil
	}
	tag, _, _ := nbt.DecodeNetwork([]byte(n.wire))
	return tag
}

// put writes the name into a saved compound, each field only when set.
func (n customName) put(tag nbt.CompoundTag) {
	if name := n.tag(); name != nil {
		tag.Set("CustomName", name)
	}
	if n.visible {
		tag.Set("CustomNameVisible", boolTag(true))
	}
}

// putMetadata writes the fields that differ from was (customName{} for a
// spawn), reporting whether it wrote any.
func (n customName) putMetadata(b *protocol.Buffer, was customName) bool {
	// The unnamed body every tick asks about stays on this inlined path.
	if n.wire == "" && was.wire == "" && n.visible == was.visible {
		return false
	}
	return n.putTurned(b, was)
}

// putTurned is putMetadata past its unnamed fast path.
func (n customName) putTurned(b *protocol.Buffer, was customName) bool {
	if n.wire != was.wire {
		b.PutMetadata(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText)
		n.putName(b)
	}
	if n.visible != was.visible {
		b.PutMetadata(protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean)
		b.PutBool(n.visible)
	}
	return n != was
}

// putName writes the optional-component value of the name field.
func (n customName) putName(b *protocol.Buffer) {
	b.PutBool(n.wire != "")
	b.PutRaw([]byte(n.wire))
}

// addFields appends the name's synched values, defaults included.
func (n customName) addFields(m *metaFields) {
	m.add(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText, n.putName)
	m.add(protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(n.visible) })
}

// metadataPayload is the Set Entity Metadata packet of a spawn whose only
// synched values are the name's, nil at the defaults.
func (n customName) metadataPayload(id int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	if !n.putMetadata(&b, customName{}) {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}
