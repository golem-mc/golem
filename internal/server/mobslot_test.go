package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/google/uuid"
)

func TestMobRecordSlotsSurviveCompactionAndReuse(t *testing.T) {
	h := newMobHerd(nil)
	var ents []*mobEntity
	for i := range 4 {
		ent := &mobEntity{mob: &entity.Mob{ID: int32(i)}, uuid: uuid.UUID{byte(i + 1)}}
		h.add(ent.mob)
		h.remember(ent)
		ents = append(ents, ent)
	}
	h.remember(ents[3])
	if len(h.records) != 4 {
		t.Fatal("filing an existing mob allocated another slot")
	}
	slot := ents[1].mob.RecordSlot()
	h.forget(ents[1].mob)
	h.remove(ents[1].mob)
	h.setHerd(h.herd[1:])
	for _, ent := range []*mobEntity{ents[0], ents[2], ents[3]} {
		if h.record(ent.mob) != ent {
			t.Fatalf("compaction lost mob %d", ent.mob.ID)
		}
	}
	if h.record(ents[1].mob) != nil || h.records[slot-1] != nil || h.byUUID[ents[1].uuid] != nil {
		t.Fatal("a forgotten mob retained a live index or record")
	}
	fresh := &mobEntity{mob: &entity.Mob{ID: 4}, uuid: uuid.UUID{5}}
	h.remember(fresh)
	h.add(fresh.mob)
	if fresh.mob.RecordSlot() != slot || len(h.records) != 4 || h.record(fresh.mob) != fresh {
		t.Fatal("a newly filed mob did not reuse the vacant record")
	}
	if h.record(ents[1].mob) != nil || h.record(&entity.Mob{}) != nil {
		t.Fatal("an unfiled mob resolved to another mob's record")
	}
	h.forget(ents[1].mob)
	if h.record(fresh.mob) != fresh {
		t.Fatal("forgetting a former occupant removed the new record")
	}
}

func TestMobRecordSlotDimensionTransfer(t *testing.T) {
	from, to := newMobHerd(nil), newMobHerd(nil)
	moving := &mobEntity{mob: &entity.Mob{}, uuid: uuid.UUID{1}}
	resident := &mobEntity{mob: &entity.Mob{}, uuid: uuid.UUID{2}}
	from.remember(moving)
	to.remember(resident)
	if from.record(resident.mob) != nil || to.record(moving.mob) != nil {
		t.Fatal("a slot resolved in a different herd")
	}
	from.forget(moving.mob)
	to.remember(moving)
	if from.record(moving.mob) != nil || to.record(moving.mob) != moving || to.record(resident.mob) != resident {
		t.Fatal("dimension transfer lost or aliased a live record")
	}
	from.remember(&mobEntity{mob: &entity.Mob{}, uuid: uuid.UUID{3}})
	to.forget(moving.mob)
	if from.records[0] == nil || to.record(resident.mob) != resident {
		t.Fatal("removing a transferred mob disturbed another record")
	}
}
