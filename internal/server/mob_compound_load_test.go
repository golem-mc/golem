package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestNativeMobCompoundLoadsEquipmentAndRiders(t *testing.T) {
	t.Parallel()
	s := &Server{}
	mount := &mobEntity{mob: entity.NewMob(entity.TypePig, 1, 4, 70, 4, 7), uuid: uuid.New()}
	rider := &mobEntity{mob: entity.NewMob(entity.TypeSkeleton, 2, 4, 70, 4, 8), uuid: uuid.New()}
	rider.mob.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1})
	mount.mob.Passengers = []*entity.Mob{rider.mob}
	book := map[*entity.Mob]*mobEntity{mount.mob: mount, rider.mob: rider}
	saved := mobTagIn(mount, book)
	list, _ := nbt.Get[nbt.List](saved.Entries(), "Passengers")
	if len(list.Items) != 1 {
		t.Fatal("missing saved rider")
	}
	if _, ok := list.Items[0].(*nbt.OrderedCompound); !ok {
		t.Fatal("rider lost native history")
	}
	if ids := passengersOfTag(saved.Entries()); len(ids) != 1 || ids[0] != rider.uuid {
		t.Fatal("native rider identity was lost")
	}
	loaded := mobOfTag(s, nil, saved.Entries())
	h := &mobHerd{}
	var woken wakeResult
	h.standVehicleRiders(s, saved.Entries(), make(map[uuid.UUID]bool), vehicleSeat{}, &woken)
	if len(woken.mobs) != 1 || woken.mobs[0].mob.MainHand.ItemID != int32(registry.ItemStone) {
		t.Fatal("vehicle lost its native rider")
	}
	riders := standPassengers(h, s, loaded, saved.Entries(), map[uuid.UUID]bool{mount.uuid: true})
	if len(riders) != 1 || riders[0].uuid != rider.uuid || riders[0].mob.MainHand.ItemID != int32(registry.ItemStone) {
		t.Fatalf("native pending rider did not load its identity and equipment: %+v", riders)
	}
}
