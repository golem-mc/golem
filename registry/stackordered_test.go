package registry

import (
	"github.com/golem-mc/golem/nbt"
	"testing"
)

func TestStoredContainerAcceptsOrderedComponentViews(t *testing.T) {
	t.Parallel()
	for _, patch := range []string{
		`{"minecraft:custom_data":{v:1,f:2,nested:{BB:1,Aa:2}}}`,
		`{"minecraft:bundle_contents":[{id:"minecraft:diamond",count:2,components:{"minecraft:custom_data":{v:1,f:2}}}]}`,
		`{"minecraft:custom_name":{text:"Key",bold:true,hover_event:{action:"show_text",value:{text:"Hello"}}}}`,
		`{"minecraft:written_book_content":{title:"Book",author:"Tester",pages:[{raw:{text:"Page",bold:1b}}],resolved:1b}}`,
		`{"minecraft:profile":{name:"Tester",properties:[{name:"textures",value:"value"}]}}`,
		`{"minecraft:equippable":{slot:"head",equip_sound:{sound_id:"minecraft:item.armor.equip_leather",range:7.0f}}}`,
		`{"minecraft:can_break":[{blocks:"minecraft:oak_log",state:{axis:{min:"x",max:"z"}}}]}`,
		`{"minecraft:potion_contents":{custom_effects:[{id:"minecraft:speed",duration:200,amplifier:1b}]}}`,
		`{"minecraft:tool":{rules:[{blocks:"#minecraft:mineable/pickaxe",speed:4.0f,correct_for_drops:true}],default_mining_speed:2.0f,damage_per_block:3}}`,
		`{"minecraft:blocks_attacks":{damage_reductions:[{base:0.0f,factor:0.5f,type:"#minecraft:is_fire"}],block_sound:"minecraft:item.shield.block"}}`,
	} {
		source := `{id:"minecraft:stick",count:1,components:` + patch + `}`
		plain, err := nbt.ParseSNBT(source)
		if err != nil {
			t.Fatal(err)
		}
		ordered, err := nbt.ParseOrderedSNBT(source)
		if err != nil {
			t.Fatal(err)
		}
		want, valid := StoredContainerStack(plain)
		got, ok := StoredContainerStack(ordered.Entries())
		if !valid || !ok {
			t.Fatalf("component refused: %s (plain %t, ordered %t)", patch, valid, ok)
		}
		if a, b := nbt.Stringify(StackTag(got)), nbt.Stringify(StackTag(want)); a != b {
			t.Errorf("%s: ordered %s, plain %s", patch, a, b)
		}
	}
}
