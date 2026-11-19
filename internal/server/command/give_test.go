package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestTextComponentMalformedAcceptsTheCodecsShapes pins the walk's text
// component check to the shapes the game's codec reads: a content form with
// the styling around it, the number a boolean reads, and the content fields
// each type carries; a list whose element is no component, a map naming no
// content, and a scalar are refused.
// vanilla: ComponentSerialization.CODEC, Style.Serializer
func TestTextComponentMalformedAcceptsTheCodecsShapes(t *testing.T) {
	accepted := []string{
		`"a"`,
		`{text:'a',color:red}`,
		`{text:'a',color:'#ff00aa'}`,
		`{text:'a',bold:true}`,
		`{text:'a',bold:1}`,
		`{text:'a',bold:1.5}`,
		`{text:'a',italic:2}`,
		`{translate:'x'}`,
		`{translate:'x',fallback:5}`,
		`{translate:'x',with:['a',5]}`,
		`{keybind:'key.jump'}`,
		`{selector:'@a'}`,
		`{score:{name:'a',objective:'b'}}`,
		`{nbt:'x',source:'storage',storage:'minecraft:x'}`,
		`{nbt:'x',block:'0 0 0'}`,
		`{nbt:'x',entity:'@e'}`,
		`{nbt:'x',entity:'@e[nbt={a:1}]'}`,
		`{nbt:'x',entity:'@e[scores={a=1}]'}`,
		`{nbt:'x',entity:'Steve'}`,
		`{nbt:'x',storage:'a:b',interpret:1b,plain:0.0d}`,
		`{nbt:'x',storage:'a:b',separator:5,interpret:1b}`,
		`{nbt:'x',entity:'00000000-0000-0000-0000-000000000000'}`,
		`{text:'a',hover_event:{action:'show_text',value:'x'}}`,
		`{text:'a',click_event:{action:'run_command',command:'/help'}}`,
		`{text:'a',click_event:{action:'show_dialog',dialog:'minecraft:server_links'}}`,
		`{text:'a',click_event:{action:'show_dialog',dialog:{type:'notice',title:'Hi'}}}`,
		`{text:'a',click_event:{action:'open_url',url:'https://example.com'}}`,
		`{text:'a',click_event:{action:'open_url',url:'http://example.com'}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone'}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',count:64}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'minecraft:damage':5}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:damage':{}}}}`,
		`{text:'a',hover_event:{action:'show_entity',id:'minecraft:zombie',uuid:'00000000-0000-0000-0000-000000000000'}}`,
		`{text:'a',hover_event:{action:'show_entity',id:'minecraft:zombie',uuid:'00000000-0000-0000-0000-000000000000',name:'Bob'}}`,
		`{text:'a',font:'minecraft:default'}`,
		`{text:'a',shadow_color:5}`,
		`{text:'a',shadow_color:[1,0,0,1]}`,
		`[{text:'a'},{text:'b'}]`,
	}
	for _, value := range accepted {
		if msg := textComponentMalformed(value); msg != "" {
			t.Errorf("textComponentMalformed(%s) = %q, want accepted", value, msg)
		}
	}
	refused := []string{
		`5`, `5b`, `true`, `[]`, `{}`, `{text:5}`, `[1]`, `[{text:'x'},5]`,
		`{text:'a',color:'foo'}`,
		`{text:'a',color:'RED'}`,
		`{text:'a',color:'#gg'}`,
		`{text:'a',extra:[]}`,
		`{text:'a',extra:[5]}`,
		`{text:'a',extra:[{}]}`,
		`{score:{}}`,
		`{score:{name:'a'}}`,
		`{nbt:'x'}`,
		`{nbt:'x',block:'foo'}`,
		`{nbt:'x',storage:'A'}`,
		`{nbt:'x',entity:''}`,
		`{text:'a',type:'bogus'}`,
		`{text:'a',type:5}`,
		`{text:'a',font:'BAD'}`,
		`{text:'a',shadow_color:'x'}`,
		`{text:'a',click_event:{}}`,
		`{text:'a',click_event:5}`,
		`{text:'a',click_event:{action:'open_file',path:'x'}}`,
		`{text:'a',click_event:{action:'open_url'}}`,
		`{text:'a',click_event:{action:'open_url',url:'foo'}}`,
		`{text:'a',click_event:{action:'open_url',url:'ftp://x'}}`,
		`{text:'a',click_event:{action:'open_url',url:'http:'}}`,
		`{text:'a',click_event:{action:'open_url',url:'http://'}}`,
		`{text:'a',click_event:{action:'open_url',url:'http://example.com/a b'}}`,
		`{text:'a',click_event:{action:'open_url',url:'http://example.com/{x}'}}`,
		`{text:'a',click_event:{action:'run_command'}}`,
		`{text:'a',click_event:{action:'run_command',command:'§c'}}`,
		`{text:'a',click_event:{action:'suggest_command',command:'a` + "\x7f" + `b'}}`,
		`{text:'a',hover_event:5}`,
		`{text:'a',hover_event:{action:'show_text'}}`,
		`{text:'a',hover_event:{action:'show_item'}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:air'}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',count:0}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',count:100}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:5}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'minecraft:bogus':1}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:bogus':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:damage':5}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'minecraft:creative_slot_lock':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:creative_slot_lock':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'minecraft:additional_trade_cost':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:additional_trade_cost':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'minecraft:map_post_processing':{}}}}`,
		`{text:'a',hover_event:{action:'show_item',id:'minecraft:stone',components:{'!minecraft:map_post_processing':{}}}}`,
		`{text:'a',hover_event:{action:'show_entity'}}`,
		`{nbt:'x',storage:'a:b',interpret:0.5d,plain:1b}`,
		`{nbt:'x',storage:'a:b',interpret:4294967296L,plain:1b}`,
		`{nbt:'a',source:5,storage:'a:b'}`,
		`{text:'a',click_event:{action:'open_url',url:'file:///x'}}`,
		`{text:'a',click_event:{action:'change_page',page:0}}`,
		`{text:'a',hover_event:{action:'show_entity',id:'minecraft:zombie',uuid:'00000000-0000-0000-0000-000000000000',name:5}}`,
	}
	for _, value := range refused {
		if msg := textComponentMalformed(value); msg == "" {
			t.Errorf("textComponentMalformed(%s) accepted a value the codec refuses", value)
		}
	}
}

func TestGiveRefusesNegativeAndNonFiniteDurationScales(t *testing.T) {
	item, ok := registry.ItemID("minecraft:potion")
	if !ok {
		t.Fatal("the registry has no potion")
	}
	for _, value := range []string{"-1", "-0.5f", "nan", "inf", "-inf", "Infinity"} {
		var stack protocol.ItemStack
		if applyComponent(&stack, item, "minecraft:potion_duration_scale", value) {
			t.Errorf("potion_duration_scale %q was accepted", value)
		}
	}
	var stack protocol.ItemStack
	if !applyComponent(&stack, item, "minecraft:potion_duration_scale", "0.5f") || stack.PotionDurationScaleOf() != 0.5 {
		t.Errorf("0.5f gave scale %v", stack.PotionDurationScaleOf())
	}
}
