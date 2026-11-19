package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func TestEntityWakeForSaveRetainsCompoundOrder(t *testing.T) {
	t.Parallel()
	arrow := &arrowEntity{uuid: uuid.New(), arrow: entity.NewArrow(1, 4, 70, 4, 0, 0, 0, false, entity.ArrowBaseDamage, entity.PickupAllowed, 0, 1)}
	saved := arrowTag(arrow)
	wantKeys := nbt.HashOrderedKeys(saved)
	h := &mobHerd{carried: make(map[world.ChunkPos][]nbt.Tag)}
	s := &Server{}
	pos := world.ChunkPos{}
	// An auto-save wake carries non-mobs until the tick can restore them.
	h.standUp(pos, []nbt.Tag{saved}, s, make(map[uuid.UUID]bool), true)
	if len(h.carried[pos]) != 1 {
		t.Fatal("save wake discarded the pending arrow")
	}
	carried, ok := h.carried[pos][0].(*nbt.OrderedCompound)
	if !ok || !slices.Equal(nbt.HashOrderedKeys(carried), wantKeys) {
		t.Fatal("save wake discarded the compound's ordering")
	}
	known := make(map[uuid.UUID]bool)
	woken := h.standUp(pos, h.carried[pos], s, known, false)
	if len(woken.arrows) != 1 || woken.arrows[0].uuid != arrow.uuid {
		t.Fatal("tick wake failed to restore the carried arrow")
	}
	if duplicate := h.standUp(pos, []nbt.Tag{saved}, s, known, false); len(duplicate.arrows) != 0 {
		t.Fatal("tick wake duplicated an already restored arrow")
	}
}
