package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func BenchmarkEntityChunkWake(b *testing.B) {
	arrow := &arrowEntity{uuid: uuid.New(), arrow: entity.NewArrow(1, 4, 70, 4, 0, 0, 0, false, entity.ArrowBaseDamage, entity.PickupAllowed, 0, 1)}
	// Disk reads decode into map compounds; pending snapshots retain ordered ones.
	tags := []nbt.Tag{nbt.PlainCopy(arrowTag(arrow))}
	s := &Server{}
	h := &mobHerd{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		woken := h.standUp(world.ChunkPos{}, tags, s, make(map[uuid.UUID]bool), false, true)
		if len(woken.arrows) != 1 {
			b.Fatal("arrow did not wake")
		}
	}
}
