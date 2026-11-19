package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

func TestMobMetadataSimulationWritersInvalidateAndSend(t *testing.T) {
	for _, test := range []struct {
		name                                 string
		kind                                 entity.MobType
		change, restore                      func(*entity.Mob)
		group                                entity.MobMetadataGroup
		index, serializer, changed, restored byte
	}{
		{"baby", entity.TypeCow, func(m *entity.Mob) { m.SetBaby(true) }, func(m *entity.Mob) { m.SetBaby(false) }, entity.MobMetadataBaby, protocol.MetadataIndexBaby, protocol.MetadataBoolean, 1, 0},
		{"fire", entity.TypeCow, func(m *entity.Mob) { m.IgniteForTicks(10) }, func(m *entity.Mob) { m.SetMetadataFireTicks(0) }, entity.MobMetadataParticles, protocol.MetadataIndexEntityFlags, protocol.MetadataByte, protocol.EntityFlagOnFire, 0},
		{"dye", entity.TypeSheep, func(m *entity.Mob) { m.Dye("red") }, func(m *entity.Mob) { m.Dye("white") }, entity.MobMetadataSheepColour, protocol.MetadataIndexSheepColour, protocol.MetadataByte, 14, 0},
		{"bee", entity.TypeBee, func(m *entity.Mob) { m.SetBeeState(true, false) }, func(m *entity.Mob) { m.SetBeeState(false, false) }, entity.MobMetadataBeeFlags, protocol.MetadataIndexBeeFlags, protocol.MetadataByte, entity.BeeFlagStung, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ent, tick, in := mobMetadataTracking(test.kind)
			ent.mob.ClearMetadataDirty()
			assert := func(value byte) {
				t.Helper()
				if !ent.mob.MetadataDirty().Has(test.group) {
					t.Fatal("simulation writer bypassed invalidation")
				}
				var want protocol.Buffer
				want.PutVarInt(ent.mob.ID)
				want.PutMetadata(test.index, int32(test.serializer))
				want.PutU8(value)
				want.EndMetadata()
				packets := metadataTrackingSends(tick, in)
				if len(packets) != 2 {
					t.Fatalf("got %d packets, want two", len(packets))
				}
				for _, p := range packets {
					if p.id != protocol.IDPlayEntityMetadata || !bytes.Equal(p.data, want.Bytes()) {
						t.Fatalf("packet %x/%x, want metadata %x", p.id, p.data, want.Bytes())
					}
				}
				ent.mob.ClearMetadataDirty()
			}
			test.change(ent.mob)
			revision := ent.mob.MetadataRevision()
			test.change(ent.mob)
			if ent.mob.MetadataRevision() != revision {
				t.Fatal("equal simulation write invalidated metadata")
			}
			assert(test.changed)
			test.restore(ent.mob)
			assert(test.restored)
			test.change(ent.mob)
			test.restore(ent.mob)
			if packets := metadataTrackingSends(tick, in); len(packets) != 0 {
				t.Fatal("reverted write sent metadata")
			}
		})
	}
}
