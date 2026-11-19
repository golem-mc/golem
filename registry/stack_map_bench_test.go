package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func BenchmarkStackMapCodec(b *testing.B) {
	for _, fixture := range []struct{ name, component string }{
		{"enchantments", `"minecraft:enchantments":{"minecraft:frost_walker":4,"minecraft:quick_charge":2,"minecraft:lure":1,"minecraft:binding_curse":5,"minecraft:riptide":3}`},
		{"state", `"minecraft:block_state":{BB:"1",Aa:"2",v:"3",f:"4"}`},
		{"debug", `"minecraft:debug_stick_state":{"minecraft:oak_stairs":"facing","minecraft:birch_stairs":"half","minecraft:stone_stairs":"shape","minecraft:spruce_stairs":"waterlogged"}`},
		{"lock", `"minecraft:lock":{components:{"minecraft:custom_data":{BB:1,Aa:2,v:3,f:4}},predicates:{"minecraft:custom_data":{BB:1,Aa:2,v:3,f:4}}}`},
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
