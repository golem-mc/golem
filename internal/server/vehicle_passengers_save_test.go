package server

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// vanilla: Entity.save, Entity.saveWithoutId
func TestChunkEntitiesVehiclePassengersPreserveRetrySnapshot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ordered   bool
		departing bool
	}{
		{"map/save", false, false},
		{"map/unload", false, true},
		{"ordered/save", true, false},
		{"ordered/unload", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
			stand := nbt.Compound{"id": nbt.String("minecraft:armor_stand"), "Invisible": nbt.Byte(1)}
			var vehicle nbt.CompoundTag = nbt.Compound{}
			if tc.ordered {
				vehicle = nbt.NewOrderedCompound(8)
			}
			vehicle.Set("id", nbt.String("minecraft:minecart"))
			vehicle.Set("UUID", nbt.UUIDToIntArray(id))
			// These keys share a Java HashMap bucket; insertion order matters.
			vehicle.Set("BB", nbt.Int(2))
			vehicle.Set("Aa", nbt.Int(1))
			vehicle.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stand}})
			original := nbt.Snapshot(vehicle)

			villager := &mobEntity{mob: entity.NewMob(entity.TypeVillager, 1, 5.5, 64, 5.5, 1)}
			pig := &mobEntity{mob: entity.NewMob(entity.TypePig, 2, 5.5, 64, 5.5, 2)}
			villager.mob.Passengers = []*entity.Mob{pig.mob}
			pig.mob.Vehicle = villager.mob
			h := &mobHerd{book: map[*entity.Mob]*mobEntity{villager.mob: villager, pig.mob: pig}}
			var steppedOff map[*mobEntity]uuid.UUID
			if tc.departing {
				steppedOff = map[*mobEntity]uuid.UUID{villager: id}
			} else {
				villager.cart = &minecartEntity{uuid: id}
			}
			want := nbt.Snapshot(vehicle).(nbt.CompoundTag)
			want.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stand, mobTagIn(villager, h.book)}})
			for range 2 {
				got := h.chunkEntities(world.ChunkPos{}, []*entity.Mob{villager.mob, pig.mob}, []nbt.Tag{vehicle}, steppedOff)
				if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
					t.Fatalf("saved %d roots, want one vehicle preserving compound history and nesting villager/pig", len(got))
				}
				if !reflect.DeepEqual(vehicle, original) {
					t.Fatal("saving passengers mutated the retry input")
				}
			}
		})
	}
}
