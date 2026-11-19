package server

import (
	"bytes"

	"github.com/golem-mc/golem/internal/protocol"
)

// metaField is one synched value on the wire: index, serializer, encoded
// value.
type metaField struct {
	index uint8
	typ   int32
	value []byte
}

// metaFields is the full set of a body's synched values, defaults included.
type metaFields []metaField

// add appends a field whose value put writes.
func (m *metaFields) add(index uint8, typ int32, put func(b *protocol.Buffer)) {
	var b protocol.Buffer
	put(&b)
	*m = append(*m, metaField{index: index, typ: typ, value: b.Bytes()})
}

// metadataChanges builds Set Entity Metadata for values that differ between
// two states, resets to default included; nil if none differ.
// vanilla: SynchedEntityData.packDirty
func metadataChanges(entityID int32, before, after metaFields) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	changed := false
	for _, f := range after {
		was := false
		for _, old := range before {
			if old.index == f.index && old.typ == f.typ && bytes.Equal(old.value, f.value) {
				was = true
				break
			}
		}
		if was {
			continue
		}
		changed = true
		b.PutMetadata(f.index, f.typ)
		b.PutRaw(f.value)
	}
	if !changed {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}
