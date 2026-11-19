package world

import (
	"github.com/golem-mc/golem/nbt"
	"testing"
)

// BenchmarkNestedBlockEntitySave covers the nested codecs after their load.
func BenchmarkNestedBlockEntitySave(b *testing.B) {
	for _, fixture := range []struct {
		name string
		kind BlockEntityKind
		data string
	}{
		{"lock", KindChest, `{lock:{components:{"minecraft:custom_data":{v:1,f:2,m:3,bo:4,nested:{BB:1,Aa:2}}},predicates:{"minecraft:custom_data":{v:3,f:4}}}}`},
		{"spawner", KindSpawner, `{SpawnData:{entity:{id:"minecraft:pig",v:1,f:2,m:3,bo:4,nested:{BB:1,Aa:2}}},SpawnPotentials:[{weight:2,data:{entity:{id:"minecraft:pig",v:1,f:2}}}]}`},
		{"pot", KindDecoratedPot, `{sherds:{right:{id:"minecraft:brick"},front:{id:"minecraft:angler_pottery_sherd"}}}`},
		{"listener", KindSculkSensor, `{listener:{event:{game_event:"minecraft:step",distance:1.0f,pos:[1.0d,2.0d,3.0d]},selector:{tick:4L},event_delay:2}}`},
		{"cursors", KindSculkCatalyst, `{cursors:[{pos:[I;1,2,3],charge:4,decay_delay:0,update_delay:2,facings:["up","north"]}]}`},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			tag, err := nbt.ParseSNBT(fixture.data)
			if err != nil {
				b.Fatal(err)
			}
			entity := newBlockEntity(fixture.kind, 2, 64, 3)
			entity.loadFrom(tag)
			b.ReportAllocs()
			for b.Loop() {
				entity.SaveTag()
			}
		})
	}
}
