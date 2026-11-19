package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMobLodestoneCompassClearsEveryEquipmentSlot(t *testing.T) {
	t.Parallel()
	for slot, name := range []string{"mainhand", "offhand", "feet", "legs", "chest", "head"} {
		t.Run(name, func(t *testing.T) {
			srv, c := villagerYard(t, servertest.OpsListing(t, "Villager"))
			recorder := servertest.NewMetadataRecorder(t, srv, c)
			// An out-of-height target clears even when its chunk is not loaded.
			packets := recorder.Run(fmt.Sprintf(`summon minecraft:cow 3.5 64 3.5 {NoAI:1b,equipment:{%s:{id:"minecraft:compass",count:1,components:{"minecraft:lodestone_tracker":{target:{dimension:"minecraft:overworld",pos:[I;0,-1000,0]},tracked:1b}}}}}`, name))
			id := servertest.SpawnedID(packets, protocol.EntityTypeCow)
			if id == 0 {
				t.Fatal("cow did not spawn")
			}
			var got protocol.ItemStack
			for _, p := range packets {
				if p.Id != protocol.IDPlayEntityEquipment {
					continue
				}
				r := protocol.NewReader(p.Data)
				if r.ReadVarInt() != id {
					continue
				}
				for {
					wireSlot := r.ReadU8()
					stack, ok := r.ReadSlot()
					if !ok {
						t.Fatal("invalid equipment stack")
					}
					if int(wireSlot&0x7f) == slot {
						got = stack
					}
					if wireSlot&0x80 == 0 || r.Err() != nil {
						break
					}
				}
				if r.Err() != nil {
					t.Fatal(r.Err())
				}
			}
			want := servertest.ItemStack(t, "minecraft:compass", 1)
			want.SetLodestoneTracker(protocol.LodestoneTracker{Tracked: true})
			if !got.Equal(want) {
				t.Fatalf("equipment slot %s = %+v, want cleared compass %+v", name, got, want)
			}
		})
	}
}
