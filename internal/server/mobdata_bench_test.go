package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func BenchmarkMobSave(b *testing.B) {
	for _, kind := range []entity.MobType{entity.TypePig, entity.TypePanda, entity.TypeSkeleton, entity.TypeVillager, entity.TypeWarden} {
		b.Run(kind.Name(), func(b *testing.B) {
			ent := &mobEntity{mob: entity.NewMob(kind, 1, 0.5, 64, 0.5, 7), uuid: uuid.New()}
			var memories string
			switch kind {
			case entity.TypeVillager:
				memories = `"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}},"minecraft:meeting_point":{value:{dimension:"minecraft:overworld",pos:[I;3,64,4]}}`
			case entity.TypeWarden:
				memories = `"minecraft:sniff_cooldown":{value:{},ttl:1000L},"minecraft:is_sniffing":{value:{}}`
			}
			if memories != "" {
				tag, err := nbt.ParseSNBT(`{NoAI:1b,NoGravity:1b,Brain:{memories:{` + memories + `}}}`)
				if err != nil {
					b.Fatal(err)
				}
				ent.mob.LoadFrom(tag, nil, nil)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = mobTagIn(ent, nil)
			}
		})
	}
}

func BenchmarkMobChunkWake(b *testing.B) {
	ent := &mobEntity{mob: entity.NewMob(entity.TypeSkeleton, 1, 4, 70, 4, 7), uuid: uuid.New()}
	ent.mob.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1})
	// Disk reads have map children; pending snapshots use native compounds.
	tags := []nbt.Tag{nbt.PlainCopy(mobTagIn(ent, nil))}
	s := &Server{}
	h := &mobHerd{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		woken := h.standUp(world.ChunkPos{}, tags, s, make(map[uuid.UUID]bool), false)
		if len(woken.mobs) != 1 || woken.mobs[0].mob.MainHand.ItemID != int32(registry.ItemStone) {
			b.Fatal("mob did not wake with equipment")
		}
	}
}
