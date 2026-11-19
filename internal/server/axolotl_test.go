package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

func TestAxolotlMetadataSpawnAndDelta(t *testing.T) {
	m := entity.NewMob(entity.TypeAxolotl, 1, 0, 64, 0, 1)
	ent := &mobEntity{mob: m}
	was := mobMetadataStateOf(m)
	m.SetAxolotlVariant(entity.AxolotlBlue)
	m.FromBucket = true
	delta := mobMetadataDeltaPayload(ent, was)
	for _, field := range [][]byte{
		{protocol.MetadataIndexAxolotlVariant, protocol.MetadataVarInt, 4},
		{protocol.MetadataIndexAxolotlFromBucket, protocol.MetadataBoolean, 1},
	} {
		if !bytes.Contains(delta, field) || !bytes.Contains(mobMetadataPayload(ent), field) {
			t.Fatalf("metadata missing %x: %x", field, delta)
		}
	}
	was = mobMetadataStateOf(m)
	m.SetAxolotlVariant(entity.AxolotlLucy)
	m.FromBucket = false
	delta = mobMetadataDeltaPayload(ent, was)
	if !bytes.Contains(delta, []byte{protocol.MetadataIndexAxolotlVariant, protocol.MetadataVarInt, 0}) || !bytes.Contains(delta, []byte{protocol.MetadataIndexAxolotlFromBucket, protocol.MetadataBoolean, 0}) {
		t.Fatalf("metadata did not reset: %x", delta)
	}
}
