package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

func TestArmadilloMetadataUsesItsStateSerializer(t *testing.T) {
	t.Parallel()
	m := entity.NewMob(entity.TypeArmadillo, 1, 0, 64, 0, 1)
	ent := &mobEntity{mob: m}
	was := mobMetadataStateOf(m)
	m.LoadFrom(nbt.Compound{"state": nbt.String("scared")}, nil, nil)
	field := []byte{protocol.MetadataIndexArmadilloState, protocol.MetadataArmadilloState, 2}
	if !bytes.Contains(mobMetadataPayload(ent), field) || !bytes.Contains(mobMetadataDeltaPayload(ent, was), field) {
		t.Fatal("spawn or delta lost the loaded armadillo state")
	}
	was = mobMetadataStateOf(m)
	m.LoadFrom(nbt.Compound{"state": nbt.String("idle")}, nil, nil)
	field[2] = 0
	if !bytes.Contains(mobMetadataDeltaPayload(ent, was), field) {
		t.Fatal("delta did not reset armadillo state to idle")
	}
}
