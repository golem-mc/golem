package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func BenchmarkStackPayloadCodec(b *testing.B) {
	for _, fixture := range []struct{ name, component string }{
		{"entity", `"minecraft:entity_data":{id:"minecraft:pig",BB:1,Aa:2,nested:{BB:3,Aa:4},v:5,f:6}`},
		{"bucket", `"minecraft:bucket_entity_data":{BB:1,Aa:2,nested:{BB:3,Aa:4},v:5,f:6}`},
		{"bees", `"minecraft:bees":[{entity_data:{id:"minecraft:bee",BB:1,Aa:2,nested:{BB:3,Aa:4}},ticks_in_hive:1,min_ticks_in_hive:2}]`},
		{"map", `"minecraft:map_decorations":{BB:{type:"minecraft:red_x",x:1.0d,z:2.0d,rotation:3.0f},Aa:{type:"minecraft:red_x",x:4.0d,z:5.0d,rotation:6.0f}}`},
	} {
		tag, err := nbt.ParseOrderedSNBT(`{id:"minecraft:stick",count:1,components:{` + fixture.component + `}}`)
		if err != nil {
			b.Fatal(err)
		}
		stack, ok := StoredStack(tag)
		if !ok {
			b.Fatal("invalid fixture", fixture.name)
		}
		b.Run(fixture.name+"/save", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				StackTag(stack)
			}
		})
		b.Run(fixture.name+"/wire", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var wire protocol.Buffer
				wire.PutSlot(stack)
			}
		})
	}
}
